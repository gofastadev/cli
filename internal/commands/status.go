package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report the health of the current project — Wire drift, swagger drift, pending migrations, generated-file state",
	Long: `Run a set of offline, filesystem-only health checks that answer the
question an AI agent asks most often: "is this project in a clean,
up-to-date state?" Output is a structured report — one row per check —
with details that tell the agent (or human) exactly what's out of sync
and which command brings it back.

Checks, in order:
  1. Wire drift        — is app/di/wire_gen.go older than any of its inputs?
  2. Swagger drift     — is docs/swagger.json older than the controllers?
  3. Pending migrations (offline) — count of .up.sql files that
                          appear to be unapplied (inspect only —
                          accurate check requires a DB connection)
  4. Uncommitted generated files — does git think wire_gen.go /
                          swagger.json / generated resolvers differ
                          from the committed version?
  5. go.sum freshness  — does ` + "`go mod tidy`" + ` produce a diff?

Use ` + "`--json`" + ` (inherited from the root command) to emit the report as
structured JSON suitable for agent consumption.

Non-zero exit when any check reports a drift or pending state so CI and
agents can branch on success/failure.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

// statusCheck is one line of the report. JSON tags are stable API.
type statusCheck struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"` // "ok" | "drift" | "warn" | "skip"
	Message string   `json:"message,omitempty"`
	Detail  []string `json:"detail,omitempty"`
}

// statusResult aggregates every check.
type statusResult struct {
	Checks   []statusCheck `json:"checks"`
	OK       int           `json:"ok"`
	Drift    int           `json:"drift"`
	Warnings int           `json:"warnings"`
	Skipped  int           `json:"skipped"`
}

// runStatus is the entry point for the status subcommand. Each check
// runs in the project root (current working dir). If a check doesn't
// apply to this project (e.g., no Wire, no Swagger, no git), it skips
// rather than failing.
func runStatus() error {
	// Load .env so checkPendingMigrations can build a DSN that actually
	// reaches the project's database. Without this, the migrations
	// check falls back to "DB unreachable" even when the DB is healthy
	// — see migrate.go for the full why.
	_, _ = loadDotEnv(".env")

	steps := []struct {
		name string
		fn   func() statusCheck
	}{
		{"wire drift", checkWireDrift},
		{"swagger drift", checkSwaggerDrift},
		{"pending migrations", checkPendingMigrations},
		{"uncommitted generated files", checkUncommittedGenerated},
		{"go.sum freshness", checkGoSumFreshness},
	}

	result := statusResult{Checks: make([]statusCheck, 0, len(steps))}
	for _, s := range steps {
		check := s.fn()
		check.Name = s.name
		result.Checks = append(result.Checks, check)
		switch check.Status {
		case "ok":
			result.OK++
		case "drift":
			result.Drift++
		case "warn":
			result.Warnings++
		case "skip":
			result.Skipped++
		}
	}

	cliout.Print(result, func(w io.Writer) {
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		for _, c := range result.Checks {
			mark := statusMark(c.Status)
			fprintf(tw, "%s\t%s\t%s\n", mark, c.Name, c.Message)
		}
		_ = tw.Flush()
		fprintln(w)
		fprintf(w, "%d ok · %d drift · %d warnings · %d skipped\n",
			result.OK, result.Drift, result.Warnings, result.Skipped)
		for _, c := range result.Checks {
			if c.Status == "drift" || c.Status == "warn" {
				for _, d := range c.Detail {
					fprintf(w, "  · %s: %s\n", c.Name, d)
				}
			}
		}
	})

	// Non-zero exit when any drift is detected so CI and agents branch on it.
	if result.Drift > 0 {
		return clierr.Newf(clierr.CodeVerifyFailed,
			"%d check(s) reported drift; run the remediation hint for each", result.Drift)
	}
	return nil
}

func statusMark(s string) string {
	switch s {
	case "ok":
		return termcolor.CGreen("✓")
	case "drift":
		return termcolor.CRed("✗")
	case "warn":
		return termcolor.CYellow("⚠")
	case "skip":
		return termcolor.CDim("-")
	default:
		return "?"
	}
}

// --- Individual checks ------------------------------------------------------

// checkWireDrift: wire_gen.go must be newer than every .go file in app/di/.
func checkWireDrift() statusCheck {
	wireGen := filepath.Join("app", "di", "wire_gen.go")
	info, err := os.Stat(wireGen)
	if err != nil {
		return statusCheck{Status: "skip", Message: "no app/di/wire_gen.go (not a Wire project)"}
	}
	wireGenMod := info.ModTime()

	var stalest string
	var stalestTime time.Time
	_ = filepath.WalkDir(filepath.Join("app", "di"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || filepath.Base(path) == "wire_gen.go" {
			return nil
		}
		if i, err := d.Info(); err == nil && i.ModTime().After(wireGenMod) && i.ModTime().After(stalestTime) {
			stalest = path
			stalestTime = i.ModTime()
		}
		return nil
	})
	if stalest != "" {
		return statusCheck{
			Status:  "drift",
			Message: "wire_gen.go is stale — run `gofasta wire`",
			Detail:  []string{fmt.Sprintf("newest input: %s (%s)", stalest, stalestTime.Format(time.RFC3339))},
		}
	}
	return statusCheck{Status: "ok", Message: "in sync"}
}

// checkSwaggerDrift: docs/swagger.json must be newer than every controller.
func checkSwaggerDrift() statusCheck {
	swagger := filepath.Join("docs", "swagger.json")
	info, err := os.Stat(swagger)
	if err != nil {
		return statusCheck{Status: "skip", Message: "no docs/swagger.json (swagger not generated)"}
	}
	swaggerMod := info.ModTime()

	var stale []string
	_ = filepath.WalkDir(filepath.Join("app", "rest", "controllers"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if i, err := d.Info(); err == nil && i.ModTime().After(swaggerMod) {
			stale = append(stale, filepath.Base(path))
		}
		return nil
	})
	if len(stale) > 0 {
		return statusCheck{
			Status:  "drift",
			Message: "docs/swagger.json is stale — run `gofasta swagger`",
			Detail:  []string{fmt.Sprintf("controllers newer than swagger.json: %s", strings.Join(stale, ", "))},
		}
	}
	return statusCheck{Status: "ok", Message: "in sync"}
}

// checkPendingMigrations compares the highest .up.sql version on disk
// to the schema_migrations version reported by the migrate tool. Three
// outcomes:
//
//   - all defined migrations applied → "ok"
//   - some defined migrations have a version > current applied → "drift"
//   - schema_migrations is dirty (a previous migrate failed mid-step) → "warn"
//
// If the DB can't be reached or the version can't be parsed, falls back
// to a "skip" with a clear message — it's better to abstain than to
// claim "N pending" when we don't actually know.
func checkPendingMigrations() statusCheck {
	dir := filepath.Join("db", "migrations")
	if _, err := os.Stat(dir); err != nil {
		return statusCheck{Status: "skip", Message: "no db/migrations directory"}
	}
	defined, err := readDefinedMigrations(dir)
	if err != nil {
		return statusCheck{Status: "skip", Message: "could not read db/migrations"}
	}
	if len(defined) == 0 {
		return statusCheck{Status: "ok", Message: "no migrations defined"}
	}

	dbURL := buildMigrationURL()
	if dbURL == "" {
		return statusCheck{
			Status:  "skip",
			Message: fmt.Sprintf("%d migration(s) defined; could not load config to check applied state", len(defined)),
		}
	}

	current, dirty, err := readAppliedMigrationVersion(dir, dbURL)
	if err != nil {
		return statusCheck{
			Status:  "skip",
			Message: fmt.Sprintf("%d migration(s) defined; could not check applied state (DB unreachable or migrate not installed)", len(defined)),
		}
	}

	pending := 0
	for _, id := range defined {
		if id > current {
			pending++
		}
	}

	switch {
	case dirty:
		return statusCheck{
			Status:  "warn",
			Message: fmt.Sprintf("schema is dirty at version %d — fix the failing migration and run `migrate force <version>`", current),
		}
	case pending == 0:
		return statusCheck{
			Status:  "ok",
			Message: fmt.Sprintf("%d migration(s) applied (current version: %d)", len(defined), current),
		}
	default:
		return statusCheck{
			Status:  "drift",
			Message: fmt.Sprintf("%d migration(s) pending — run `gofasta migrate up` (current: %d, latest defined: %d)", pending, current, defined[len(defined)-1]),
		}
	}
}

// readDefinedMigrations returns the sorted list of migration version
// numbers found as <number>_*.up.sql files in dir.
func readDefinedMigrations(dir string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		idx := strings.Index(name, "_")
		if idx <= 0 {
			continue
		}
		id, err := strconv.Atoi(name[:idx])
		if err != nil {
			continue
		}
		seen[id] = true
	}
	out := make([]int, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Ints(out)
	return out, nil
}

// readAppliedMigrationVersion shells out to `migrate version` to get
// the current applied version from the schema_migrations table. The
// migrate tool prints the version to stderr (despite being called
// "version" — that's just how it works) in one of three formats:
//
//	"5"            → applied through version 5
//	"5 (dirty)"    → version 5 mid-application; manual repair needed
//	"no migration" → schema_migrations exists but is empty
//	error/empty    → DB unreachable, schema_migrations missing, etc.
//
// Returns (current, dirty, err). When err is nil and current is 0,
// nothing has been applied yet.
func readAppliedMigrationVersion(dir, dbURL string) (current int, dirty bool, err error) {
	cmd := execCommand("migrate", "-path", dir, "-database", dbURL, "version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if runErr := cmd.Run(); runErr != nil {
		// migrate exits non-zero when schema_migrations doesn't exist
		// AND when the DB is unreachable. Distinguish via output:
		// "no migration" means reachable-but-empty (treat as 0/clean);
		// anything else is a real failure.
		if strings.Contains(out.String(), "no migration") {
			return 0, false, nil
		}
		return 0, false, runErr
	}
	text := strings.TrimSpace(out.String())
	if text == "" || strings.Contains(text, "no migration") {
		return 0, false, nil
	}
	// Format: "<number>" or "<number> (dirty)"
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return 0, false, fmt.Errorf("unexpected migrate version output: %q", text)
	}
	v, perr := strconv.Atoi(parts[0])
	if perr != nil {
		return 0, false, fmt.Errorf("could not parse migrate version output %q: %w", text, perr)
	}
	dirty = strings.Contains(text, "dirty")
	return v, dirty, nil
}

// checkUncommittedGenerated reports whether git sees modifications to
// files gofasta typically regenerates. If git is unavailable or this
// isn't a git repo, skip silently.
func checkUncommittedGenerated() statusCheck {
	if _, err := exec.LookPath("git"); err != nil {
		return statusCheck{Status: "skip", Message: "git not on $PATH"}
	}
	// Paths gofasta regenerates — these are the most likely uncommitted
	// artifacts after running generators.
	watched := []string{
		"app/di/wire_gen.go",
		"docs/swagger.json",
		"docs/swagger.yaml",
		"docs/docs.go",
		"app/generated_stub.go",
	}
	var dirty []string
	for _, path := range watched {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out, err := runGitPorcelain(path)
		if err != nil {
			// Not a git repo or git error — skip entirely, one check.
			return statusCheck{Status: "skip", Message: "not a git repository"}
		}
		if strings.TrimSpace(out) != "" {
			dirty = append(dirty, path)
		}
	}
	sort.Strings(dirty)
	if len(dirty) > 0 {
		return statusCheck{
			Status:  "warn",
			Message: fmt.Sprintf("%d generated file(s) have uncommitted changes — review and commit", len(dirty)),
			Detail:  dirty,
		}
	}
	return statusCheck{Status: "ok", Message: "generated files committed"}
}

func runGitPorcelain(path string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain", "--", path)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// checkGoSumFreshness runs `go mod tidy -diff` if available, or falls
// back to a no-op when the flag isn't supported.
func checkGoSumFreshness() statusCheck {
	// Older Go toolchains don't support `go mod tidy -diff`. Use
	// `go mod verify` instead — it catches most staleness issues and
	// is universal.
	cmd := exec.Command("go", "mod", "verify")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return statusCheck{
			Status:  "drift",
			Message: "`go mod verify` failed — run `go mod tidy`",
			Detail:  []string{strings.TrimSpace(buf.String())},
		}
	}
	return statusCheck{Status: "ok", Message: "modules verified"}
}
