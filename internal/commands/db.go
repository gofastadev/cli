package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Destructive database utilities (reset, rebuild, re-seed)",
	Long: `Grouping for operations that wipe or recreate schema — separated from
` + "`gofasta migrate`" + ` because these commands are destructive and should not be
run accidentally against a shared database.

Subcommands:
  reset    Drop every table, re-apply all migrations, and re-run seeds

All subcommands read config.yaml for connection details and delegate to
golang-migrate for schema work. They respect the same environment variables
and driver fallbacks as ` + "`gofasta migrate`" + `.`,
}

var dbResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Drop every table, re-apply all migrations, and re-run seeds",
	Long: `Rebuild the database from scratch. Executes three steps in order:

  1. ` + "`migrate drop -f`" + ` — drops every table, including schema_migrations
  2. ` + "`migrate up`" + `     — re-applies every migration from version 1
  3. ` + "`go run ./app/main seed`" + ` — runs the project's seed functions

Useful during development when schema churn makes incremental migrations
awkward, or when you need a known-good fixture state.

Use --skip-seed to stop after step 2 (schema reset without fixtures).

This command is destructive and irreversible — all existing data is lost.
Never run it against a shared or production database. ` + "`gofasta doctor`" + ` will
warn if the configured database URL looks like a non-local host.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		skipSeed, _ := cmd.Flags().GetBool("skip-seed")
		return runDBReset(skipSeed)
	},
}

func init() {
	dbResetCmd.Flags().Bool("skip-seed", false, "Skip running database seeds after migration")
	dbCmd.AddCommand(dbResetCmd)
	rootCmd.AddCommand(dbCmd)
}

// dbResetStep is one phase of `gofasta db reset` (drop / up / seed).
type dbResetStep struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // "ok" | "fail" | "skip"
	Message    string `json:"message,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// dbResetResult is the structured payload for `--json` consumers.
type dbResetResult struct {
	Steps      []dbResetStep `json:"steps"`
	DurationMs int64         `json:"duration_ms"`
}

func runDBReset(skipSeed bool) error {
	// See comment in migrate.go: scaffolded projects keep DB
	// credentials in .env (not config.yaml), and compose maps host
	// port 5433 → container 5432. Loading .env here lets the migrate
	// shell-out reach the DB and lets the spawned `go run ./app/main
	// seed` child see the same connection details via os.Environ().
	_, _ = loadDotEnv(".env")

	dbURL := buildMigrationURL()
	if dbURL == "" {
		return fmt.Errorf("failed to load config — ensure config.yaml exists")
	}

	totalStart := time.Now()
	result := dbResetResult{}

	steps := []struct {
		name string
		fn   func() error
	}{
		{"drop", func() error {
			return runDBStep("drop", "migrate", "-path", "db/migrations", "-database", dbURL, "drop", "-f")
		}},
		{"migrate up", func() error {
			return runDBStep("migrate up", "migrate", "-path", "db/migrations", "-database", dbURL, "up")
		}},
	}
	if !skipSeed {
		steps = append(steps, struct {
			name string
			fn   func() error
		}{"seed", func() error {
			return runDBStep("seed", "go", "run", "./app/main", "seed")
		}})
	}

	for _, s := range steps {
		stepStart := time.Now()
		err := s.fn()
		entry := dbResetStep{
			Name:       s.name,
			Status:     "ok",
			DurationMs: time.Since(stepStart).Milliseconds(),
		}
		if err != nil {
			entry.Status = "fail"
			entry.Message = err.Error()
			result.Steps = append(result.Steps, entry)
			result.DurationMs = time.Since(totalStart).Milliseconds()
			cliout.Print(result, func(w io.Writer) { printDBResetSummary(w, result) })
			// Preserve the original error wording for callers depending on it.
			switch s.name {
			case "drop":
				return fmt.Errorf("drop failed: %w", err)
			case "migrate up":
				return fmt.Errorf("migration up failed: %w", err)
			default:
				return fmt.Errorf("seeding failed: %w", err)
			}
		}
		result.Steps = append(result.Steps, entry)
	}

	result.DurationMs = time.Since(totalStart).Milliseconds()
	cliout.Print(result, func(w io.Writer) { printDBResetSummary(w, result) })
	return nil
}

// runDBStep runs one migrate/seed shell-out, streaming output to the
// user (in text mode) and announcing the step with a ▶ line. In --json
// mode output is suppressed so the final payload is the only thing
// written to stdout.
func runDBStep(label, name string, args ...string) error {
	if !cliout.JSON() {
		fmt.Println(termcolor.Step("Running %s", label))
	}
	cmd := execCommand(name, args...)
	if cliout.JSON() {
		cmd.Stdout = &bytes.Buffer{}
		cmd.Stderr = &bytes.Buffer{}
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
	}
	return cmd.Run()
}

// printDBResetSummary writes one line per step plus a final tally.
func printDBResetSummary(w io.Writer, r dbResetResult) {
	for _, s := range r.Steps {
		switch s.Status {
		case "ok":
			_, _ = fmt.Fprintln(w, termcolor.Success("%s complete (%dms)", s.Name, s.DurationMs))
		case "fail":
			_, _ = fmt.Fprintln(w, termcolor.Fail("%s failed: %s", s.Name, s.Message))
		case "skip":
			_, _ = fmt.Fprintln(w, termcolor.Info("%s skipped", s.Name))
		}
	}
	_, _ = fmt.Fprintf(w, "\n%d step(s) · %dms\n", len(r.Steps), r.DurationMs)
}
