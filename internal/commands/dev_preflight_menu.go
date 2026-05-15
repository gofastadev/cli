package commands

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
)

// ─────────────────────────────────────────────────────────────────────
// Interactive preflight menu.
//
// Called when runPreflight reports at least one unreachable dep. The
// menu presents four explicit recovery paths:
//
//   [1] Enter a different connection string for the failing service
//   [2] Start the missing service(s) in Docker
//   [3] Run app without db (degrades gracefully; explicit warnings)
//   [4] Cancel and exit
//
// Loop semantics: options [1] and [2] re-run the probes after applying
// their action; if probes still fail, the menu re-renders. Options [3]
// and [4] terminate the menu unconditionally.
//
// Non-TTY environments (CI, piped stdin) skip the menu entirely and
// return menuCancel after printing the same actionable text the menu
// would have shown. This keeps `gofasta dev` scriptable.
// ─────────────────────────────────────────────────────────────────────

// menuOutcome is the terminal verdict of runPreflightMenu.
type menuOutcome int

const (
	// menuOK means the menu loop resolved successfully: either the
	// initial probes already passed (no menu shown) or a retry after
	// [1]/[2] reported all-OK. The pipeline proceeds normally.
	menuOK menuOutcome = iota

	// menuRunWithoutDB means the user picked option [3] "Run app
	// without db". The pipeline continues with the noDB flag set;
	// migrations + seeds skip. The scaffolded `ProvideDB` falls back
	// to an in-memory SQLite stand-in so the app boots — DB-touching
	// endpoints return 5xx (no schema), non-DB endpoints work.
	menuRunWithoutDB

	// menuCancel means the user picked option [4], OR we're in a
	// non-TTY environment, OR the user pressed Ctrl+C inside the
	// menu. The pipeline exits non-zero.
	menuCancel
)

// ── Package-level seams ─────────────────────────────────────────────

var (
	// menuInputFn returns the line-buffered reader the menu reads
	// choices from. Production: os.Stdin. Tests: a bytes.NewReader
	// with a pre-canned sequence of choices.
	menuInputFn = func() io.Reader { return os.Stdin }

	// menuOutputFn returns the writer for menu prompts. Production:
	// os.Stdout. Tests inspect the captured output.
	menuOutputFn = func() io.Writer { return os.Stdout }

	// menuIsTTYFn reports whether stdin is a terminal. Non-TTY
	// short-circuits the menu and returns menuCancel.
	menuIsTTYFn = isStdinTTY

	// menuReprobeFn re-runs the full preflight after a recovery
	// action. Default = runPreflight; tests inject deterministic
	// canned probe results.
	menuReprobeFn = runPreflight

	// menuStartServicesFn brings up the named compose services
	// detached, used by option [2]. Default = production
	// startServices with ALL discovered profiles active, so
	// profile-gated services (e.g. `cache`, `queue`) actually start
	// instead of being silently filtered out by compose. Tests stub
	// the docker calls.
	menuStartServicesFn = func(names []string) error {
		profiles, _ := detectComposeProfiles()
		return startServices(names, profiles)
	}

	// menuWaitHealthyFn waits for the named services to become
	// healthy. Default = wraps the real waitHealthy with a short
	// timeout and a no-op progress callback; tests inject a no-op.
	menuWaitHealthyFn = defaultMenuWaitHealthy
)

// isStdinTTY reports whether os.Stdin is connected to a terminal.
// Uses the same termIsTerminalFn seam the keyboard listener uses so
// tests can override both via one knob.
func isStdinTTY() bool {
	return termIsTerminalFn(int(os.Stdin.Fd()))
}

// defaultMenuWaitHealthy waits up to 30s for the named services to
// report healthy. Progress messages flow through the human emitter
// so users see live feedback while compose pulls images / starts
// containers.
func defaultMenuWaitHealthy(names []string) error {
	profiles, _ := detectComposeProfiles()
	available, hasHealth, err := detectComposeServices(profiles, true)
	if err != nil {
		return fmt.Errorf("compose config: %w", err)
	}
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	subset := make([]string, 0, len(names))
	for _, n := range available {
		if wanted[n] {
			subset = append(subset, n)
		}
	}
	return waitHealthy(subset, hasHealth, defaultWaitTimeout, func(name, state string, _ time.Duration) {
		_, _ = fmt.Fprintf(menuOutputFn(), "  → %s %s\n", name, state)
	})
}

// runPreflightMenu is the entry point called by the dev pipeline when
// at least one probe came back unreachable. Returns the resolved
// outcome; the caller branches on it to either continue (menuOK),
// degrade (menuRunWithoutDB), or exit (menuCancel).
//
// The function is self-driving: it loops internally on retries, prints
// progress, and only returns when the user picks a terminal option or
// all probes report OK.
//
// `startedServices` is the cumulative list of compose services the
// menu brought up via option [2] across all iterations. The caller
// merges these into plan.services.selected so the pipeline's teardown
// closure stops them on q / Ctrl+C. Without this hand-off the
// services start but never stop — leaking containers between dev
// sessions.
func runPreflightMenu(initial []probeResult) (outcome menuOutcome, startedServices []string) {
	current := initial

	// Non-TTY: print actionable text + bail. Skipping the interactive
	// loop keeps CI scripts deterministic.
	if !menuIsTTYFn() {
		printPreflightFailures(current)
		_, _ = fmt.Fprintln(menuOutputFn(), "")
		_, _ = fmt.Fprintln(menuOutputFn(), "  Non-interactive shell detected — menu skipped.")
		_, _ = fmt.Fprintln(menuOutputFn(), "  Resolve the unreachable dependency manually, then re-run `gofasta dev`.")
		return menuCancel, nil
	}

	reader := bufio.NewReader(menuInputFn())
	for {
		printPreflightFailures(current)
		_, _ = fmt.Fprintln(menuOutputFn(), "")
		_, _ = fmt.Fprintln(menuOutputFn(), "How would you like to proceed?")
		_, _ = fmt.Fprintln(menuOutputFn(), "")
		_, _ = fmt.Fprintln(menuOutputFn(), "  [1] Enter a different connection string for the failing service")
		_, _ = fmt.Fprintf(menuOutputFn(), "  [2] Start the missing service(s) in Docker (%s)\n", failingDepsCSV(current))
		_, _ = fmt.Fprintln(menuOutputFn(), "  [3] Run app without db")
		_, _ = fmt.Fprintln(menuOutputFn(), "      ⚠ Migrations will NOT run")
		_, _ = fmt.Fprintln(menuOutputFn(), "      ⚠ Endpoints that touch the database will return 5xx")
		_, _ = fmt.Fprintln(menuOutputFn(), "      ⚠ The app's *gorm.DB is an in-memory SQLite stub — no schema, no persistence")
		_, _ = fmt.Fprintln(menuOutputFn(), "  [4] Cancel and exit")
		_, _ = fmt.Fprintln(menuOutputFn(), "")
		_, _ = fmt.Fprint(menuOutputFn(), "Choose [1-4]: ")

		line, err := reader.ReadString('\n')
		if err != nil {
			_, _ = fmt.Fprintln(menuOutputFn(), "\n  ⚠ stdin closed; exiting")
			return menuCancel, startedServices
		}
		choice := strings.TrimSpace(line)

		switch choice {
		case "1":
			kvs, err := menuActionEnterConnString(reader, current)
			if err != nil {
				_, _ = fmt.Fprintf(menuOutputFn(), "  ⚠ could not apply connection string: %v\n", err)
			}
			current = menuReprobeFn()
			if !hasUnreachable(current) {
				// Reprobe passed — the override works. Offer to
				// persist it to .env under the project's prefix so
				// the next `gofasta dev` session doesn't make the
				// user re-paste a long DSN. Default is no — the
				// session-scoped path stays the security-safe
				// default; persistence is one explicit keystroke.
				if len(kvs) > 0 {
					if err := promptPersistConnString(reader, kvs); err != nil {
						_, _ = fmt.Fprintf(menuOutputFn(), "  ⚠ persist failed: %v\n", err)
					}
				}
				return menuOK, startedServices
			}
		case "2":
			// Remember the services the menu attempted to start —
			// even if waitHealthy later times out, `docker compose
			// up -d` already created containers, and teardown must
			// stop them so a failed option-2 attempt doesn't leak
			// a half-started db container into the next dev session.
			attempted := mapFailingDepsToServices(current)
			startedServices = mergeServices(startedServices, attempted)
			if err := menuActionStartInDocker(current); err != nil {
				_, _ = fmt.Fprintf(menuOutputFn(), "  ⚠ could not start in Docker: %v\n", err)
			}
			current = menuReprobeFn()
			if !hasUnreachable(current) {
				return menuOK, startedServices
			}
		case "3":
			return menuRunWithoutDB, startedServices
		case "4":
			return menuCancel, startedServices
		default:
			_, _ = fmt.Fprintf(menuOutputFn(), "  ⚠ invalid choice %q — pick a number from 1 to 4\n", choice)
		}
	}
}

// mergeServices appends `add` to `base`, dropping duplicates. Stable
// order: existing entries first, new ones in insertion order. Used to
// accumulate "services started by the menu" across iteration loops
// without re-counting a service that's already in the list.
func mergeServices(base, add []string) []string {
	seen := make(map[string]bool, len(base))
	for _, s := range base {
		seen[s] = true
	}
	out := base
	for _, s := range add {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// printPreflightFailures renders the per-dep status header above the
// menu. Each unreachable dep shows the endpoint + reason; ok and
// not-configured deps are still shown so the user has full context.
func printPreflightFailures(results []probeResult) {
	_, _ = fmt.Fprintln(menuOutputFn(), "⚠ Preflight failed:")
	for _, r := range results {
		switch r.Status {
		case probeOK:
			_, _ = fmt.Fprintf(menuOutputFn(), "   ✓ %s reachable\n", r.Dep)
		case probeNotConfigured:
			_, _ = fmt.Fprintf(menuOutputFn(), "   • %s not configured\n", r.Dep)
		case probeUnreachable:
			ep := r.Endpoint
			if ep == "" {
				ep = "(no endpoint)"
			}
			_, _ = fmt.Fprintf(menuOutputFn(), "   ✗ %s unreachable at %s (%s)\n",
				r.Dep, ep, condense(r.Reason))
		}
	}
}

// failingDepsCSV returns a comma-joined list of the unreachable deps
// for inclusion in the "[2] Start in Docker (...)" prompt line. Used
// only for display; the actual service names mapped from these deps
// are resolved in menuActionStartInDocker.
func failingDepsCSV(results []probeResult) string {
	names := make([]string, 0, len(results))
	for _, r := range results {
		if r.Status == probeUnreachable {
			names = append(names, r.Dep)
		}
	}
	return strings.Join(names, ", ")
}

// condense trims trailing whitespace and collapses multi-line error
// messages to a single line so the menu stays readable.
func condense(s string) string {
	s = strings.TrimSpace(s)
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before + "..."
	}
	return s
}

// menuActionEnterConnString implements menu option [1].
//
// The user types a full database URL (e.g. postgres://user:pass@host:port/db).
// We parse it, validate the URL shape, then override the in-process
// env vars that configutil reads for the database section. The dev
// pipeline's outer runDev loop re-loads .env on every iteration, so
// the override is scoped to the current run — exactly what a "try
// this connection real quick" UX should do.
//
// Returns the KV map that was applied (so the caller can offer to
// persist it to .env after a successful reprobe), or nil on error.
// The returned map keys are NOT prefixed — the caller decides which
// prefix to write under (project-specific for persistence; every
// prefix for in-process override).
func menuActionEnterConnString(reader *bufio.Reader, _ []probeResult) (map[string]string, error) {
	_, _ = fmt.Fprintln(menuOutputFn(), "")
	_, _ = fmt.Fprint(menuOutputFn(), "  Connection string (e.g. postgres://user:pass@host:port/db): ")

	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	raw := strings.TrimSpace(line)
	if raw == "" {
		return nil, fmt.Errorf("empty input")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("URL must include scheme and host")
	}

	kvs := connStringToDatabaseKVs(parsed)

	// Apply under EVERY prefix configutil's loader knows about. The
	// loader applies GOFASTA_ first, then the project-specific prefix
	// (derived from go.mod) which OVERRIDES GOFASTA_ on conflict. If
	// we only set GOFASTA_ vars, a project that declared its own
	// .env-loaded prefix (e.g. IRONJISENDAV2_DATABASE_HOST=localhost)
	// would shadow our override and the reprobe would still see the
	// stale value — exactly the bug a user encountering this menu is
	// most likely to hit.
	for _, prefix := range configutil.EnvPrefixes() {
		for k, v := range kvs {
			_ = os.Setenv(prefix+k, v)
		}
	}

	termcolor.PrintSuccess("override applied — re-probing…")
	return kvs, nil
}

// connStringToDatabaseKVs converts a parsed connection-string URL into
// the un-prefixed DATABASE_* KV map the menu's callers consume. Returns
// only fields the URL actually carries — empty fields are omitted so
// the caller doesn't accidentally clobber a config.yaml default with
// an empty string.
//
// Two special cases:
//   - missing port → fill in the driver's protocol default (5432 etc.)
//     so the previous .env's PORT doesn't leak through.
//   - sslmode in the query string → propagate as DATABASE_SSLMODE so
//     managed Postgres services (Neon, RDS, etc.) that REQUIRE TLS
//     don't silently fall back to "disable".
func connStringToDatabaseKVs(parsed *url.URL) map[string]string {
	driver := strings.TrimSuffix(parsed.Scheme, "ql")
	host, port := splitHostPort(parsed.Host)
	if port == "" {
		port = defaultPortForDriver(driver)
	}
	user, password := "", ""
	if parsed.User != nil {
		user = parsed.User.Username()
		password, _ = parsed.User.Password()
	}

	kvs := map[string]string{"DATABASE_DRIVER": driver}
	pairs := []struct{ key, val string }{
		{"DATABASE_HOST", host},
		{"DATABASE_PORT", port},
		{"DATABASE_USER", user},
		{"DATABASE_PASSWORD", password},
		{"DATABASE_NAME", strings.TrimPrefix(parsed.Path, "/")},
		{"DATABASE_SSLMODE", parsed.Query().Get("sslmode")},
	}
	for _, p := range pairs {
		if p.val != "" {
			kvs[p.key] = p.val
		}
	}
	return kvs
}

// promptPersistConnString asks whether the just-applied override
// should be written to .env under the project-specific prefix.
// Default answer (bare Enter) is NO — the session-scoped path is the
// security-safe default, and we don't want a careless paste of
// production credentials to end up on disk by accident. `y`/`Y`/`yes`
// triggers the write.
//
// Persistence writes ONLY under the project prefix (e.g. DATA_DATABASE_HOST
// for a project whose go.mod is `module data`), never under GOFASTA_.
// The project's .env should look like a normal project file; toolkit
// branding on disk is the lock-in pattern the gofasta identity rules
// explicitly forbid.
//
// If no project prefix can be derived (no go.mod, or malformed
// module path), we refuse with a clear message rather than silently
// falling back to GOFASTA_.
//
// Errors writing the file are reported to the user but do NOT roll
// back the in-memory override — the session continues with the
// override applied, just without disk persistence.
func promptPersistConnString(reader *bufio.Reader, kvs map[string]string) error {
	if len(kvs) == 0 {
		return nil
	}
	_, _ = fmt.Fprint(menuOutputFn(), "  Save these settings to .env for next session? [y/N]: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		termcolor.PrintStep("  (skipped persistence — this session only)")
		return nil
	}

	prefix := configutil.ProjectEnvPrefix()
	if prefix == "" {
		return fmt.Errorf("cannot persist: no go.mod found in this directory, so no project prefix to write under")
	}

	prefixed := make(map[string]string, len(kvs))
	for k, v := range kvs {
		prefixed[prefix+k] = v
	}

	if err := mergeIntoDotEnv(".env", prefixed); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	_, _ = fmt.Fprintf(menuOutputFn(), "  ✓ saved to .env (%s settings updated in place)\n", prefix)
	return nil
}

// defaultPortForDriver returns the canonical TCP port for a postgres-
// family driver name (matching the values pkg/config.SetupDB falls
// back to when none is configured). Called from option [1] when the
// user's URL omits an explicit port so we don't carry the previous
// .env's PORT through into the rebuilt connection string. Returns ""
// for drivers without a network port (sqlite) or unknown drivers,
// which the caller treats as "leave PORT unset".
func defaultPortForDriver(driver string) string {
	switch driver {
	case "postgres":
		return "5432"
	case "mysql":
		return "3306"
	case "sqlserver":
		return "1433"
	case "clickhouse":
		return "9000"
	default:
		return ""
	}
}

// splitHostPort splits a host[:port] string into its parts. Returns
// the host and an empty port if no colon present. Used by the conn-
// string parser; net.SplitHostPort errors on missing port which we
// want to treat as "no port override".
func splitHostPort(hostport string) (host, port string) {
	idx := strings.LastIndex(hostport, ":")
	if idx < 0 {
		return hostport, ""
	}
	// Guard against IPv6 [::1]:5432 — the last colon is correct there.
	return hostport[:idx], hostport[idx+1:]
}

// menuActionStartInDocker implements menu option [2].
//
// Maps the unreachable dep names to compose service names (database
// → "db", cache → "cache", queue → "queue"), verifies docker is
// available, runs `docker compose up -d <services>`, and waits for
// the services to report healthy. Returns nil on success; the menu
// then re-runs the probes via menuReprobeFn.
//
// The dep→service mapping is intentionally hardcoded: these are the
// scaffold's canonical service names, and the menu only fires for
// deps the preflight knows about (database / cache / queue). Custom
// services like `lavinmq` go through the user's explicit `--services`
// flag, not the menu.
func menuActionStartInDocker(results []probeResult) error {
	if !composeAvailableFn() {
		return fmt.Errorf("docker / docker compose not available — install Docker Desktop: https://docs.docker.com/get-docker/")
	}

	services := mapFailingDepsToServices(results)
	if len(services) == 0 {
		return fmt.Errorf("no failing services to start")
	}

	equiv := "gofasta dev --services " + strings.Join(services, ",")
	_, _ = fmt.Fprintf(menuOutputFn(), "  ℹ Equivalent flag: %s\n", equiv)

	for _, name := range services {
		_, _ = fmt.Fprintf(menuOutputFn(), "  → starting %s in Docker…\n", name)
	}
	if err := menuStartServicesFn(services); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}
	if err := menuWaitHealthyFn(services); err != nil {
		return fmt.Errorf("services did not become healthy: %w", err)
	}
	return nil
}

// mapFailingDepsToServices translates the abstract dep name from a
// probeResult (database/cache/queue) to the compose service name the
// scaffold uses (db/cache/queue). Returns only the services for deps
// in the unreachable state.
func mapFailingDepsToServices(results []probeResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		if r.Status != probeUnreachable {
			continue
		}
		switch r.Dep {
		case "database":
			out = append(out, "db")
		case "cache":
			out = append(out, "cache")
		case "queue":
			out = append(out, "queue")
		}
	}
	return out
}
