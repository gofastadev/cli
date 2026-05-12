package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var devFlagValues devFlags

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Run the local dev environment — host-first by default, optional Docker services via --services",
	Long: `Run your gofasta project's local development environment.

Default mode (no flags): the app runs on host with Air hot reload.
Before Air starts, gofasta probes your declared dependencies
(database, cache, queue from config.yaml + .env). If any dep is
unreachable, an interactive menu offers four recovery paths:

  [1] Enter a different connection string for the failing service
  [2] Start the missing service(s) in Docker (equivalent to --services <name>)
  [3] Run app without db (warns about no migrations / no persistence)
  [4] Cancel and exit

To pre-declare which compose services should run in Docker:

  gofasta dev --services db                # db in Docker, app on host
  gofasta dev --services db,cache,queue    # supporting services in Docker, app on host
  gofasta dev --services db,app            # db + app in Docker, app foregrounded
  gofasta dev --services all               # everything compose.yaml declares (full stack)

The mental model: anything listed in --services runs in Docker. If
"app" is in the list, the app runs in a foreground container instead
of on host with Air. The list scales to any service compose.yaml
declares — your own "lavinmq" or "elasticsearch" works the moment
you add it to compose.yaml, no CLI release needed.

While the pipeline is running, the terminal accepts these single-key
shortcuts (disable with --no-keyboard, auto-disabled when stdin is
not a TTY):
  r, R    restart the entire pipeline from scratch
  q, Q    quit gofasta dev (same as Ctrl+C)
  h, H, ? print the keybinding help

Pipeline stages:
  1. Load .env                  — overlay onto config.yaml
  2. Resolve plan               — validate --services against compose.yaml
  3. Service start (if any)     — docker compose up -d <selected>
  4. Health-wait                — poll each service until healthy
  5. Preflight                  — probe db/cache/queue; menu on failure
  6. Migrate                    — migrate up (delegated inside the app
                                   container when "app" is in --services;
                                   skipped under no-db mode)
  7. Seed                       — optional; runs ` + "`gofasta seed`" + `
  8. Air on host, OR foreground compose up <app>
  9. Teardown                   — on SIGINT/SIGTERM, stop services

Every step emits a structured event when --json is set, so agents and
CI tooling can branch on facts instead of log strings.

DEPRECATED: --all-in-docker is a one-release alias for --services all.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// `--all-in-docker` is the deprecated alias for `--services all`.
		// We rewrite it here (before parseServicesList) so the rest of
		// the pipeline only sees the canonical form.
		if devFlagValues.allInDocker && devFlagValues.servicesRaw == "" {
			termcolor.PrintWarn("--all-in-docker is deprecated; use --services all (silently mapped for one release)")
			devFlagValues.servicesRaw = "all"
		}
		devFlagValues.servicesList = parseServicesList(devFlagValues.servicesRaw)
		return runDev(devFlagValues)
	},
}

func init() {
	rootCmd.AddCommand(devCmd)

	f := devCmd.Flags()
	f.StringVar(&devFlagValues.servicesRaw, "services", "",
		"comma-separated list of compose services to start in Docker (e.g. 'db', 'db,cache,queue'); use 'all' for every service compose.yaml declares (full stack including app). Empty = host-only.")
	f.BoolVar(&devFlagValues.noMigrate, "no-migrate", false,
		"skip running migrate up after services become healthy")
	f.BoolVar(&devFlagValues.noTeardown, "no-teardown", false,
		"leave compose services running on exit (default: stop them)")
	f.BoolVar(&devFlagValues.keepVolumes, "keep-volumes", true,
		"preserve named volumes on teardown (default: true)")
	f.BoolVar(&devFlagValues.fresh, "fresh", false,
		"drop every compose volume before starting — forces a clean DB state")
	f.StringVar(&devFlagValues.profile, "profile", "",
		"additional docker compose profile to activate")
	f.DurationVar(&devFlagValues.waitTimeout, "wait-timeout", defaultWaitTimeout,
		"how long to wait for compose services to report healthy")
	f.StringVar(&devFlagValues.envFile, "env-file", ".env",
		"path to the .env file to load before starting Air")
	f.StringVar(&devFlagValues.port, "port", "",
		"override the PORT env var passed to Air / the app binary")
	f.BoolVar(&devFlagValues.rebuild, "rebuild", false,
		"force Air to do a rebuild cycle before first serve")
	f.BoolVar(&devFlagValues.seed, "seed", false,
		"run seeders after migrations (equivalent to running `gofasta seed` post-start)")
	f.BoolVar(&devFlagValues.dryRun, "dry-run", false,
		"print the resolved plan and exit without touching anything")
	f.BoolVar(&devFlagValues.attachLogs, "attach-logs", false,
		"stream `docker compose logs -f` alongside Air (service-prefixed)")
	f.BoolVar(&devFlagValues.dashboard, "dashboard", false,
		"start the local dev dashboard — an HTML debug page with routes, health, and live service state")
	f.IntVar(&devFlagValues.dashboardPort, "dashboard-port", 9090,
		"port for the dev dashboard HTTP server")
	f.BoolVar(&devFlagValues.allInDocker, "all-in-docker", false,
		"DEPRECATED — use --services all. This flag will be removed in the next release.")
	f.BoolVar(&devFlagValues.noKeyboard, "no-keyboard", false,
		"disable the interactive keyboard layer (r=restart, q=quit, h=help); use when stdin is piped or for non-interactive sessions")
}

// runDev is the orchestration entrypoint. It owns three top-level
// concerns:
//
//   - the keyboard listener (raw-mode stdin → restart/quit signals),
//     which must persist across pipeline iterations so the user can
//     press R again immediately after a restart completes;
//   - the restart loop, which re-runs runDevPipeline from scratch each
//     time the pipeline returns restart=true (i.e. the user pressed R);
//   - exit-time terminal restoration (deferred), so a panic does not
//     leave the user's terminal in raw mode.
//
// Tests that exercise the pipeline shape call runDev directly — under
// `go test`, os.Stdin is a pipe (not a TTY) so the keyboard layer
// auto-skips and runDev behaves exactly like the old single-pass body.
func runDev(flags devFlags) error {
	keySignals, restoreKB, _ := startKeyboardListener(os.Stdin, flags.noKeyboard)
	defer restoreKB()

	emitter := newDevEmitter(jsonOutput)

	for iter := 1; ; iter++ {
		if iter > 1 {
			emitter.Info(fmt.Sprintf("⟳ restarting from scratch (iteration %d)", iter))
		}
		restart, err := runDevPipeline(flags, keySignals, emitter)
		if err != nil {
			return err
		}
		if !restart {
			return nil
		}
	}
}

// runDevPipeline is the single-pass dev pipeline. Each call goes
// through every stage from .env load to Stage 8, then either exits
// (restart=false) or signals the outer loop to re-run (restart=true).
//
// Broken into clearly-named stages so the pipeline reads top-to-bottom.
// Each stage consults flags to decide whether to execute; each emits
// one or more events via the devEmitter so humans see status lines and
// agents see JSON events.
//
//nolint:gocognit,gocyclo // Linear pipeline; breaking it up would obscure the ordering invariants.
func runDevPipeline(flags devFlags, keySignals <-chan keyboardSignal, emitter devEmitter) (bool, error) {
	// --- Stage 0: load .env ------------------------------------------------
	// Re-loaded each iteration so editing .env and pressing R picks
	// up the new values without re-invoking gofasta dev from outside.
	if loaded, err := loadDotEnv(flags.envFile); err != nil {
		emitter.Warn(fmt.Sprintf("%s present but could not be loaded: %v", flags.envFile, err))
	} else if loaded > 0 {
		emitter.Info(fmt.Sprintf("loaded %d variables from %s", loaded, flags.envFile))
	}
	if flags.port != "" {
		_ = os.Setenv("PORT", flags.port)
	}

	// --- Stage 1: resolve services -----------------------------------------
	// Decide what we'd do — even in non-dry-run mode, we build the plan
	// before touching anything so a failure here surfaces without side
	// effects.
	plan, err := resolveDevPlan(flags)
	if err != nil {
		return false, err
	}

	if flags.dryRun {
		printDevPlan(plan, emitter)
		return false, nil
	}

	// --- Stage 2: preflight ------------------------------------------------
	if plan.orchestrate {
		if !composeAvailableFn() {
			return false, clierr.New(clierr.CodeDevDockerUnavailable,
				"docker or docker compose is not available")
		}
		docker, compose := detectVersions()
		emitter.Preflight(docker, compose)
	}

	// --- Stage 3: fresh volumes (optional) ---------------------------------
	if plan.orchestrate && flags.fresh {
		emitter.Info("dropping compose volumes (--fresh)")
		if err := resetVolumes(); err != nil {
			emitter.Warn(fmt.Sprintf("could not drop volumes: %v — continuing", err))
		}
	}

	// --- Stage 4: start services -------------------------------------------
	// Under --all-in-docker we deliberately START the app in foreground
	// later (Stage 8) instead of as a detached daemon. The app's stdout
	// then attaches directly to the user's terminal — same UX as running
	// Air on the host, just inside a container. So filter the app out of
	// the detached-up list here; supporting services (db, cache, queue)
	// stay daemonized because their output isn't useful in the foreground.
	detachedServices := plan.services.selected
	if plan.inDocker {
		detachedServices = removeService(plan.services.selected, appServiceName)
	}

	if plan.orchestrate && len(detachedServices) > 0 {
		for _, name := range detachedServices {
			emitter.ServiceStart(name)
		}
		if err := startServices(detachedServices, plan.profiles); err != nil {
			return false, clierr.Wrap(clierr.CodeDevServiceUnhealthy, err,
				"failed to start compose services")
		}

		if err := waitHealthy(detachedServices, plan.services.hasHealth, flags.waitTimeout,
			func(name, state string, elapsed time.Duration) {
				if strings.HasPrefix(state, "running/healthy") || state == "running/" {
					emitter.ServiceHealthy(name, elapsed)
				}
			}); err != nil {
			return false, clierr.Wrap(clierr.CodeDevServiceUnhealthy, err, err.Error())
		}
	}

	// Teardown runs exactly once on exit, unless --no-teardown is set.
	// `--keep-volumes=false` upgrades the teardown from `stop` (preserve
	// containers + volumes) to `down -v` (destroy both). The default keeps
	// volumes so the next `gofasta dev` reuses the primed database.
	var teardownDone bool
	runTeardown := func(reason string) {
		if teardownDone || flags.noTeardown {
			return
		}
		teardownDone = true
		if plan.orchestrate && len(plan.services.selected) > 0 {
			var err error
			var mode string
			if flags.keepVolumes {
				err = stopServices(plan.services.selected)
				mode = "stopped"
			} else {
				err = resetVolumes()
				mode = "destroyed"
			}
			if err == nil {
				emitter.Shutdown(mode, 0)
			} else {
				emitter.Shutdown(mode+"-failed", 1)
			}
		} else {
			emitter.Shutdown(reason, 0)
		}
	}
	defer runTeardown("clean")

	// --- Stage 5: preflight (dep connectivity probes) ----------------------
	// After any --services-named compose services are up + healthy, we
	// probe the app's declared deps (database / cache / queue from
	// config.yaml + .env). When a probe reports unreachable AND the
	// user is in a TTY, the interactive menu offers four recovery
	// paths (enter conn string, start in docker, run without db,
	// cancel). Non-TTY shells skip the menu and fail loud.
	//
	// The probes intentionally run AFTER service startup so a user
	// who passed `--services db` doesn't get a spurious "db
	// unreachable" warning before the just-started container has had
	// a moment to settle.
	results := runPreflight()
	if hasUnreachable(results) {
		switch runPreflightMenu(results) {
		case menuOK:
			// Recovery succeeded; proceed.
		case menuRunWithoutDB:
			flags.noDB = true
		case menuCancel:
			return false, clierr.New(clierr.CodeDevPreflightCancel,
				"preflight unresolved")
		}
	}

	// --- Stage 6: migrations -----------------------------------------------
	// Under in-docker mode the app container's CMD already runs
	// migrate against db:5432 before starting Air (see
	// deployments/docker/dev.dockerfile.tmpl), so a host-side run would
	// be a wasteful double-attempt and would force the user to have
	// the `migrate` CLI on $PATH. We emit MigrateDelegated (NOT
	// MigrateSkipped) so the message reflects what's really happening:
	// migrations ARE running — just inside the container, and their
	// output streams to the foreground via Stage 8.
	//
	// noDB mode (menu option [3]) skips migrate entirely with an
	// explicit "running without db" message.
	switch {
	case flags.noDB:
		emitter.MigrateSkipped("running without db (menu option [3])")
	case plan.inDocker:
		emitter.MigrateDelegated("running inside the app container")
	case !flags.noMigrate:
		if applied, err := runMigrationsWithCount(); err != nil {
			emitter.MigrateSkipped(err.Error())
		} else {
			emitter.MigrateOK(applied)
		}
	}

	// --- Stage 7: seed (optional) ------------------------------------------
	// Skipped automatically in noDB mode — no DB means nothing to seed.
	if flags.seed && !flags.noDB {
		if err := runSeedDelegation(); err != nil {
			emitter.Warn(fmt.Sprintf("seed failed: %v", err))
		} else {
			emitter.Info("seeders completed")
		}
	}

	// --- Stage 8: optional side-processes ----------------------------------
	// --attach-logs: stream docker compose logs alongside Air output.
	// --dashboard:   spin up the debug HTTP server on dashboardPort.
	// Both register shutdown hooks so they stop cleanly with the pipeline.
	//
	// Under in-docker mode the foreground stream IS the app container's
	// log stream (set up below in Stage 9), so an explicit --attach-logs
	// here would just duplicate work; warn and let Stage 9 own it.
	var sideCancels []func()
	if flags.attachLogs && plan.orchestrate && len(plan.services.selected) > 0 && !plan.inDocker {
		sideCancels = append(sideCancels, startLogStreamer(plan.services.selected))
	}
	if flags.attachLogs && plan.inDocker {
		emitter.Warn("--attach-logs is implicit when the app runs in docker (its logs are already in the foreground); pass --attach-logs only to multiplex backing services' logs alongside")
	}

	// In noDB mode print a loud banner so the user remembers why
	// DB-touching endpoints are about to 5xx. We use the existing
	// Warn channel so JSON consumers see the event in structured form.
	if flags.noDB {
		emitter.Warn("🔶 NO-DB MODE — migrations skipped; endpoints touching the database will return 5xx; data is not persisted across restarts")
	}

	// --- Stage 8: Air ------------------------------------------------------
	port := configutil.GetPort()
	if flags.port != "" {
		port = flags.port
	}
	portInt, _ := strconv.Atoi(port)
	urls := airURLs(port)

	if flags.dashboard {
		sideCancels = append(sideCancels, startDashboard(flags.dashboardPort, portInt, &plan.services, emitter))
	}

	if plan.inDocker {
		// Containerized mode: supporting services are already running
		// (Stage 4 brought them up with `compose up -d` minus the app).
		// Now we start the app in the FOREGROUND via `compose up app`
		// so its stdout/stderr attach directly to the user's terminal.
		// The container's CMD runs migrate then Air; both stream live
		// into the dev terminal — same UX as host-mode Air, just inside
		// a container.
		//
		// --attach-logs additionally tails the supporting services'
		// stdout via a sideband `compose logs -f` streamer so multi-
		// service debugging works without leaving the foreground.
		emitter.AirInDocker(portInt, urls)
		if flags.attachLogs {
			supporting := removeService(plan.services.selected, appServiceName)
			sideCancels = append(sideCancels, startLogStreamer(supporting))
		}
		restart, err := runInDockerForeground(runTeardown, keySignals)
		for _, c := range sideCancels {
			c()
		}
		return restart, err
	}

	emitter.Air(portInt, urls)
	restart, err := runAir(flags, runTeardown, keySignals)
	for _, c := range sideCancels {
		c()
	}
	return restart, err
}

// runInDockerForeground starts `docker compose up <app>` as a
// foreground child whose stdout/stderr inherit gofasta dev's terminal.
// The container's CMD (migrate + Air) then streams directly to the
// user's screen — same UX as host-mode Air, no log-streamer middleman.
//
// The function blocks until one of:
//
//   - SIGINT/SIGTERM (Ctrl+C from the terminal or `kill`)
//   - sigKeyboardQuit on keySignals (the user pressed Q)
//   - sigKeyboardRestart on keySignals (the user pressed R)
//   - the foreground compose process exits on its own (app crashed,
//     compose itself died, or the container's CMD returned)
//
// Returns true only when the user pressed R. All other paths return
// false so the outer pipeline loop exits cleanly.
//
// Stdin is deliberately NOT piped to the child. The keyboard listener
// in dev_keyboard.go owns stdin in cbreak mode; piping it through
// would either race the listener for bytes or confuse `docker compose
// up`'s own (unused) stdin handling.
func runInDockerForeground(teardown func(string), keySignals <-chan keyboardSignal) (bool, error) {
	cmd := execCommand("docker", "compose", "up", appServiceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// cmd.Stdin intentionally left nil — see function doc.

	if err := cmd.Start(); err != nil {
		return false, clierr.Wrap(clierr.CodeDevServiceUnhealthy, err,
			"failed to start app container in foreground")
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	return runInDockerSupervisor(cmd, exited, teardown, keySignals), nil
}

// runInDockerSupervisor blocks on a select over four channels and
// translates each event into a teardown reason. Extracted from
// runInDockerForeground so tests can drive every branch with an
// already-started (or fake) *exec.Cmd plus a synthesized `exited`
// channel — no real docker process required.
//
// Returns true only when the user pressed R, so the outer pipeline
// loop knows to re-run. Every other path returns false (clean exit).
func runInDockerSupervisor(
	cmd *exec.Cmd,
	exited <-chan error,
	teardown func(string),
	keySignals <-chan keyboardSignal,
) bool {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	interruptCompose := func() {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
	}

	select {
	case <-sigChan:
		interruptCompose()
		<-exited
		teardown("interrupted")
		return false
	case sig := <-keySignals:
		interruptCompose()
		<-exited
		if sig == sigKeyboardRestart {
			teardown("restart")
			return true
		}
		teardown("quit")
		return false
	case <-exited:
		// Compose / the app container exited on its own — crash, OOM,
		// or the container's CMD returned. Tear down the rest of the
		// stack so a half-up state doesn't linger.
		teardown("app-exited")
		return false
	}
}

// devPlan is what resolveDevPlan builds before any side effect runs.
// Passed to both the dry-run printer and the real execution path, so
// both paths see an identical picture of what's about to happen.
type devPlan struct {
	orchestrate bool        // run the compose pipeline at all
	inDocker    bool        // --all-in-docker: app + Air run inside the app container
	profiles    []string    // compose profiles to activate (cache + queue auto-on, plus --profile)
	services    devServices // resolved service set (may be empty)
}

// resolveProfiles builds the list of compose profiles to activate for
// this run. The default set is empty; users opt in with --profile.
// The cache + queue profiles, previously auto-on, are gone — they
// belonged to the now-removed auto-orchestrate model.
func resolveProfiles(flags devFlags) []string {
	if flags.profile == "" {
		return nil
	}
	return []string{flags.profile}
}

// inDockerMode reports whether the app should run in a foreground
// container (true when `app` appears in the selected service list)
// vs on host with Air. Takes the resolved list (post-`all`-expansion)
// so callers don't have to think about alias normalization.
func inDockerMode(selected []string) bool {
	for _, s := range selected {
		if s == appServiceName {
			return true
		}
	}
	return false
}

func resolveDevPlan(flags devFlags) (devPlan, error) {
	// `--services` without compose.yaml is a configuration error —
	// can't bring services up if there's no file to read from.
	if len(flags.servicesList) > 0 && !composeFileExists() {
		return devPlan{}, clierr.New(clierr.CodeDevComposeNotFound,
			"--services was set but no compose.yaml found in the project root")
	}

	// No services requested AND no compose.yaml: host-only Air, no
	// orchestration. Preflight will still check the dep connection
	// strings via Stage 1.
	if len(flags.servicesList) == 0 {
		return devPlan{orchestrate: false}, nil
	}

	// At this point flags.servicesList is non-empty AND compose.yaml
	// exists. We need to query compose for the available service list
	// and validate every name in --services against it.

	profiles := resolveProfiles(flags)
	available, hasHealth, err := detectComposeServices(profiles, true)
	if err != nil {
		return devPlan{}, clierr.Wrap(clierr.CodeDevComposeNotFound, err,
			"could not read compose configuration")
	}

	// Validate every name in --services against the compose-declared
	// list. Returns a typo suggestion when the name is "close enough".
	selected, err := selectServices(available, flags.servicesRaw)
	if err != nil {
		return devPlan{}, clierr.New(clierr.CodeDevServiceUnknown, err.Error())
	}

	// In-docker mode (app in selected list) cannot coexist with a
	// filesystem-path replace directive in go.mod — the docker build
	// context can't see paths outside the project. Check AFTER
	// selectServices so the `--services all` alias resolves to the
	// full list before this gate decides whether the app is in scope.
	inDocker := inDockerMode(selected)
	if inDocker {
		if replaces, _ := findLocalReplacesFn("go.mod"); len(replaces) > 0 {
			lines := make([]string, 0, len(replaces))
			for _, r := range replaces {
				lines = append(lines, fmt.Sprintf("    %s => %s", r.Module, r.Path))
			}
			return devPlan{}, clierr.New(clierr.CodeDevLocalReplace,
				fmt.Sprintf("go.mod has filesystem-path replace directives that the docker build cannot resolve:\n%s",
					strings.Join(lines, "\n")))
		}
	}

	return devPlan{
		orchestrate: true,
		inDocker:    inDocker,
		profiles:    profiles,
		services: devServices{
			available: available,
			selected:  selected,
			profiles:  profiles,
			hasHealth: hasHealth,
		},
	}, nil
}

func printDevPlan(plan devPlan, emitter devEmitter) {
	if plan.orchestrate {
		emitter.Info(fmt.Sprintf("orchestrate=true in_docker=%t profiles=%v selected=%v",
			plan.inDocker, plan.profiles, plan.services.selected))
	} else {
		emitter.Info("orchestrate=false (no compose.yaml or --no-services)")
	}
}

// detectVersions returns best-effort version strings for docker and
// docker compose. Used for the preflight event — non-critical, so any
// detection failure just returns "unknown".
func detectVersions() (docker, compose string) {
	docker = captureVersionLine(execCommand("docker", "version", "--format", "{{.Client.Version}}"))
	if docker == "" {
		docker = "unknown"
	}
	compose = captureVersionLine(execCommand("docker", "compose", "version", "--short"))
	if compose == "" {
		compose = "unknown"
	}
	return docker, compose
}

// captureVersionLine runs a prepared *exec.Cmd and returns the first
// non-empty line of stdout trimmed. Returns "" on any failure so the
// preflight event can fall back to "unknown" without a panic.
func captureVersionLine(cmd *exec.Cmd) string {
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	return strings.TrimSpace(line)
}

// runMigrationsWithCount re-uses the existing runMigrations but also
// tries to extract a count of applied migrations from the migrate CLI
// output. The golang-migrate CLI prints one line per applied step to
// stderr in the form "N/u migration_name (duration)" — counting those
// is a good-enough approximation of "how many ran".
func runMigrationsWithCount() (int, error) {
	if _, err := execLookPath("migrate"); err != nil {
		return 0, errors.New("migrate CLI not found on $PATH")
	}
	dbURL := configutil.BuildMigrationURL()

	var buf bytes.Buffer
	cmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		// If the output says "no change", treat it as a zero-applied success
		// rather than an error — migrate exits 0 in that case anyway, but
		// we want this branch to be explicit.
		return 0, clierr.Wrapf(clierr.CodeDevMigrationFailed, err,
			"migrate up failed:\n%s", strings.TrimSpace(buf.String()))
	}

	applied := strings.Count(buf.String(), "/u ")
	return applied, nil
}

// runSeedDelegation shells out to the project's own seed command. The
// seed code path lives in the scaffolded project (not the CLI), so we
// invoke it the same way `gofasta seed` does: via the project's main
// binary with the `seed` subcommand.
func runSeedDelegation() error {
	cmd := execCommand("go", "run", "./app/main", "seed")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// airURLs builds the per-transport URL set for the running project.
// GraphQL / swagger endpoints are included only if the project actually
// exposes them (detected via filesystem markers), so the URL set never
// lies about what's live.
func airURLs(port string) map[string]string {
	urls := map[string]string{"rest": "http://localhost:" + port}
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		urls["graphql"] = "http://localhost:" + port + "/graphql"
		urls["playground"] = "http://localhost:" + port + "/graphql-playground"
	}
	if _, err := os.Stat("docs/swagger.json"); err == nil {
		urls["swagger"] = "http://localhost:" + port + "/swagger/index.html"
	}
	urls["metrics"] = "http://localhost:" + port + "/metrics"
	urls["health"] = "http://localhost:" + port + "/health"
	return urls
}

// runAir execs `go tool air` and wires SIGINT/SIGTERM through so Ctrl+C
// tears down services before the process exits.
//
// If --rebuild is set, the Air build cache directory (tmp/ by default,
// configured via .air.toml) is deleted first so the next `go tool air`
// invocation rebuilds from scratch rather than reusing a stale binary.
// Air has no "force rebuild" flag of its own — deleting the tmp dir is
// the officially-documented way.
// removeAllFn is a package-level seam over os.RemoveAll so tests can
// exercise the post-RemoveAll error branch (in practice RemoveAll("tmp")
// rarely fails).
var removeAllFn = os.RemoveAll

func runAir(flags devFlags, teardown func(string), keySignals <-chan keyboardSignal) (bool, error) {
	if flags.rebuild {
		// tmp/ is Air's default build directory; projects that
		// customize .air.toml may use a different path, but clearing
		// the default is a safe best-effort.
		if err := removeAllFn("tmp"); err != nil {
			// A missing tmp dir is the expected state on first run.
			// Any other failure is non-fatal: Air will still run; the
			// developer just won't get the forced-rebuild guarantee.
			_ = err
		}
	}
	args := []string{"tool", "air"}

	if _, err := execLookPath("go"); err != nil {
		return false, clierr.New(clierr.CodeDevAirNotInstalled,
			"Go toolchain not on $PATH")
	}

	airCmd := execCommand("go", args...)
	airCmd.Stdout = os.Stdout
	airCmd.Stderr = os.Stderr
	// Deliberately do NOT pipe os.Stdin to Air. The keyboard listener
	// in dev_keyboard.go owns stdin (in raw mode) for r/q/h shortcuts,
	// and Air does not read stdin for any interactive feature — it
	// reloads on filesystem events. Letting Air keep stdin would either
	// fight the listener for bytes or, depending on terminal mode,
	// cause Air to burn CPU on EOF.

	// Inject GOFLAGS=-tags=devtools so Air's internal `go build`
	// compiles the scaffold's app/devtools/devtools_enabled.go file
	// (and excludes devtools_stub.go). Merges with any existing GOFLAGS
	// value in the environment so projects that rely on custom GOFLAGS
	// for other purposes aren't clobbered.
	//
	// Append to whatever Env the caller already set on the command
	// (tests use a fake exec helper that populates Env with subprocess
	// markers); starting from os.Environ() when no Env was set
	// preserves the regular pass-through behavior.
	if airCmd.Env == nil {
		airCmd.Env = os.Environ()
	}
	airCmd.Env = append(airCmd.Env, appendTag(os.Getenv("GOFLAGS"), "devtools"))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// done lets the signal-handler goroutine exit when Air finishes
	// naturally (no signal received). Without this the goroutine
	// leaks for the lifetime of the test process, which cumulatively
	// times out `go test -race ./...` across the many tests that
	// exercise runDev's happy paths.
	done := make(chan struct{})
	defer close(done)

	// restartFlag is set by the signal-handler goroutine when the
	// caller pressed R. Read by the post-Run block to decide whether
	// to return restart=true and let the outer loop re-run the
	// pipeline.
	var restartFlag atomicBool
	go airSignalHandler(sigChan, keySignals, done, airCmd, teardown, &restartFlag)

	err := airCmd.Run()
	restart := restartFlag.Load()
	// Air exits non-zero when it receives SIGINT. Treat a signal-triggered
	// exit as a successful shutdown rather than a pipeline failure.
	if err != nil && airCmd.ProcessState != nil && airCmd.ProcessState.Exited() {
		if isSignaledExit(airCmd.ProcessState) {
			return restart, nil
		}
	}
	if err != nil {
		return restart, clierr.Wrap(clierr.CodeDevAirNotInstalled, err,
			"air exited with error")
	}
	return restart, nil
}

// isSignaledExit reports whether a process exited due to a signal.
// Extracted from runAir so tests can stub it.
var isSignaledExit = func(ps *os.ProcessState) bool {
	if ps == nil {
		return false
	}
	ws, ok := ps.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled()
}

// airSignalHandler is the goroutine body from runAir. Extracted into a
// named function so tests can drive it directly without racing the
// real air process.
//
// Multiplexes three kinds of input:
//
//   - sigChan (OS SIGINT/SIGTERM): SIGINT the Air process and tear
//     down. Pipeline exits cleanly.
//   - keySignals = sigKeyboardRestart: same as Ctrl+C semantics but
//     sets restartFlag so the outer loop re-runs the pipeline.
//   - keySignals = sigKeyboardQuit: same as Ctrl+C semantics, no
//     restart.
//
// restartFlag is the seam through which runAir learns whether the
// child's SIGINT was caused by the user pressing R; it is the cleanest
// way to communicate from a goroutine without wrapping the return path
// in extra channels.
func airSignalHandler(
	sigChan <-chan os.Signal,
	keySignals <-chan keyboardSignal,
	done <-chan struct{},
	airCmd *exec.Cmd,
	teardown func(string),
	restartFlag *atomicBool,
) {
	select {
	case <-done:
		// Air finished on its own; no signal or keypress occurred.
		// Nothing to clean up here — the deferred teardown in runAir
		// owns the compose-stack stop, and signal.Stop releases the
		// OS signal channel.
		return
	case <-sigChan:
		if airCmd.Process != nil {
			_ = airCmd.Process.Signal(os.Interrupt)
		}
		teardown("interrupted")
	case sig := <-keySignals:
		if sig == sigKeyboardRestart {
			restartFlag.Store(true)
			if airCmd.Process != nil {
				_ = airCmd.Process.Signal(os.Interrupt)
			}
			teardown("restart")
			return
		}
		if airCmd.Process != nil {
			_ = airCmd.Process.Signal(os.Interrupt)
		}
		teardown("quit")
	}
}

// atomicBool is a tiny sync/atomic.Bool stand-in scoped to the
// runAir/airSignalHandler exchange. We keep our own type rather than
// pulling sync/atomic across the call sites so the goroutine
// interaction stays obvious in this file (and so runAir's signature
// does not pollute call sites with sync/atomic.Bool pointers).
type atomicBool struct {
	v atomic.Bool
}

// Store atomically writes b.
func (a *atomicBool) Store(b bool) { a.v.Store(b) }

// Load atomically returns the current value.
func (a *atomicBool) Load() bool { return a.v.Load() }

// appendTag merges a build tag into an existing GOFLAGS string. If the
// existing value already contains a -tags= fragment, we splice the new
// tag into it (comma-separated, no dupes). Otherwise we append a fresh
// -tags=<name> fragment. The returned string has the GOFLAGS= prefix
// and is suitable for dropping into os.Environ(). Kept generic (rather
// than hard-coded to "devtools") so future stages can layer on other
// tags — for instance, an observability-heavy `-tags=profiling` mode.
func appendTag(existing, tag string) string {
	// Normalize the incoming value. Both "GOFLAGS=..." and just "..."
	// variants come through the test helpers; we always return a
	// "GOFLAGS=..." string.
	val := strings.TrimPrefix(existing, "GOFLAGS=")

	if !strings.Contains(val, "-tags=") {
		if val == "" {
			return "GOFLAGS=-tags=" + tag
		}
		return "GOFLAGS=" + val + " -tags=" + tag
	}

	// Splice the tag into the existing -tags= fragment.
	parts := strings.Fields(val)
	for i, p := range parts {
		if !strings.HasPrefix(p, "-tags=") {
			continue
		}
		existingTags := strings.TrimPrefix(p, "-tags=")
		for _, t := range strings.Split(existingTags, ",") {
			if t == tag {
				return "GOFLAGS=" + val // already present
			}
		}
		parts[i] = "-tags=" + existingTags + "," + tag
		break
	}
	return "GOFLAGS=" + strings.Join(parts, " ")
}

// Legacy helpers kept for backward-compat with other files that still
// reference them. runMigrations is the original best-effort entrypoint
// used elsewhere in the codebase; leaving it here avoids churning
// callers outside the dev command.
func runMigrations() error {
	if _, err := execLookPath("migrate"); err != nil {
		return fmt.Errorf("migrate CLI not found on $PATH — install with:\n" +
			"  go install -tags 'postgres mysql sqlite3 sqlserver clickhouse' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1")
	}
	dbURL := configutil.BuildMigrationURL()
	if err := runMigrateUp(dbURL); err == nil {
		return nil
	}
	termcolor.PrintHint("Database not ready, retrying in 2 seconds...")
	time.Sleep(2 * time.Second)
	return runMigrateUp(dbURL)
}

func runMigrateUp(dbURL string) error {
	cmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
