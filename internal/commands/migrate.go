package commands

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/gofastadev/cli/internal/commands/configutil"
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

func runMigration(direction string) error {
	dbURL := buildMigrationURL()
	if dbURL == "" {
		return fmt.Errorf("failed to load config — ensure config.yaml exists")
	}

	migrateCmd := execCommand("migrate",
		"-path", "db/migrations",
		"-database", dbURL,
		direction,
	)
	migrateCmd.Stdout = os.Stdout
	migrateCmd.Stderr = os.Stderr

	slog.Info("running migration", "direction", direction)
	if err := migrateCmd.Run(); err != nil {
		return fmt.Errorf("migration %s failed: %w", direction, err)
	}
	slog.Info("migration completed", "direction", direction)
	return nil
}
