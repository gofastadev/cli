package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply or rollback SQL schema migrations against the configured database",
	Long: `Run golang-migrate against the database defined in config.yaml, using the
SQL files under db/migrations/.

The CLI builds the database URL from the active config (driver, host, port,
credentials, dsn overrides) and shells out to the ` + "`migrate`" + ` binary — you must
have it installed and on your $PATH (` + "`gofasta doctor`" + ` will check).

Subcommands:
  up     Apply every migration whose version is newer than the current schema
  down   Rollback the most recently applied migration (single step)

Create migrations with ` + "`gofasta g migration <Resource>`" + ` or ` + "`gofasta g scaffold`" + `,
which both write paired .up.sql / .down.sql files into db/migrations/.`,
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply every pending migration in db/migrations/ (idempotent)",
	Long: `Run all migration files whose version is newer than the last version recorded
in the schema_migrations table. Safe to re-run: already-applied migrations are
skipped. Fails fast on the first migration that errors, leaving the schema at
the last successful version.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigration("up")
	},
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Rollback the single most recently applied migration",
	Long: `Rollback the most recent migration by executing its paired .down.sql file.
This is a single-step rollback — run it repeatedly (or use ` + "`gofasta db fresh`" + `)
to unwind multiple versions. Destructive operations in the .down.sql are not
reversible, so review the SQL before running against a shared database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigration("down")
	},
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	rootCmd.AddCommand(migrateCmd)
}

// buildMigrationURL is a package-level seam over
// configutil.BuildMigrationURL so tests can drive the empty-URL
// defensive branch.
var buildMigrationURL = configutil.BuildMigrationURL

// migrateResult is the structured payload emitted by runMigration —
// suitable for `--json` consumers (CI, agents) and rendered by the
// human textFn for interactive use.
type migrateResult struct {
	Direction  string `json:"direction"`
	Status     string `json:"status"` // "ok" | "fail"
	Message    string `json:"message,omitempty"`
	Output     string `json:"output,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

func runMigration(direction string) error {
	// Load .env BEFORE building the migration URL. config.yaml in
	// scaffolded projects ships with in-container defaults (host:
	// localhost, port: 5432 — what the app sees from inside the
	// compose network); the .env file is where the host-side
	// overrides live (e.g. PORT=5433 because docker maps host
	// 5433 → container 5432, plus DATABASE_USER/PASSWORD/NAME).
	// Without this, migrate builds a URL that points at port 5432
	// on the host (where nothing is listening) and fails with
	// "connection refused" even when the DB is fully healthy.
	// `gofasta dev` and `gofasta doctor` both load .env this way;
	// migrate must do the same.
	_, _ = loadDotEnv(".env")

	dbURL := buildMigrationURL()
	if dbURL == "" {
		return fmt.Errorf("failed to load config — ensure config.yaml exists")
	}

	// In text mode we want progress AS the migrate tool runs, but in
	// --json mode the consumer wants exactly one machine-readable
	// payload at the end. So we capture the migrate output, then route
	// it through cliout.Print at the end with the right shape.
	var captured bytes.Buffer
	if !cliout.JSON() {
		fmt.Println(termcolor.Step("Applying migrations (%s)", direction))
	}

	// `migrate down` (no count) prompts "Are you sure you want to
	// apply ALL down migrations?" and rolls back everything. The
	// command's docs promise single-step rollback, so we always
	// rollback exactly one step. Multi-step rollbacks should go
	// through `gofasta db reset` (or run migrate directly).
	args := []string{"-path", "db/migrations", "-database", dbURL, direction}
	if direction == "down" {
		args = append(args, "1")
	}
	cmd := execCommand("migrate", args...)
	if cliout.JSON() {
		cmd.Stdout = &captured
		cmd.Stderr = &captured
	} else {
		// Stream output live in human mode AND tee into the buffer so
		// the final payload (used for the final ✓/✗ summary line) has
		// access to it if needed. Wire stdin too — `migrate down`
		// (without a step count) prompts "Are you sure you want to
		// apply all down migrations? [y/N]"; without stdin attached
		// the tool reads EOF, defaults to N, and exits non-zero.
		cmd.Stdout = io.MultiWriter(os.Stdout, &captured)
		cmd.Stderr = io.MultiWriter(os.Stderr, &captured)
		cmd.Stdin = os.Stdin
	}

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	result := migrateResult{
		Direction:  direction,
		Status:     "ok",
		Output:     captured.String(),
		DurationMs: elapsed,
	}
	if runErr != nil {
		result.Status = "fail"
		result.Message = runErr.Error()
	}

	cliout.Print(result, func(w io.Writer) {
		if runErr == nil {
			_, _ = fmt.Fprintln(w, termcolor.Success("Migration %s complete (%dms)", direction, elapsed))
		} else {
			_, _ = fmt.Fprintln(w, termcolor.Fail("Migration %s failed: %s", direction, runErr.Error()))
		}
	})

	if runErr != nil {
		return fmt.Errorf("migration %s failed: %w", direction, runErr)
	}
	return nil
}
