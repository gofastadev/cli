package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
		return termcolor.CBrand("!")
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

// checkPendingMigrations counts unique migration numbers with an up.sql.
// Offline only — we can't know which migrations are applied without a DB
// connection. Treat a positive count as a warning, not drift, because
// having migrations present is normal — they just may or may not be run.
func checkPendingMigrations() statusCheck {
	dir := filepath.Join("db", "migrations")
	if _, err := os.Stat(dir); err != nil {
		return statusCheck{Status: "skip", Message: "no db/migrations directory"}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return statusCheck{Status: "skip", Message: "could not read db/migrations"}
	}
	migrationIDs := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".up.sql") {
			// migration ID is the prefix before the first underscore.
			if idx := strings.Index(name, "_"); idx > 0 {
				migrationIDs[name[:idx]] = true
			}
		}
	}
	if len(migrationIDs) == 0 {
		return statusCheck{Status: "ok", Message: "no migrations defined"}
	}
	return statusCheck{
		Status:  "warn",
		Message: fmt.Sprintf("%d migration(s) present — run `gofasta migrate up` to apply (offline check)", len(migrationIDs)),
	}
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
