package commands

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/gofastadev/cli/internal/commands/configutil"
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

func runDBReset(skipSeed bool) error {
	dbURL := configutil.BuildMigrationURL()
	if dbURL == "" {
		return fmt.Errorf("failed to load config — ensure config.yaml exists")
	}

	// Step 1: Drop everything
	slog.Info("dropping all tables")
	dropCmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "drop", "-f")
	dropCmd.Stdout = os.Stdout
	dropCmd.Stderr = os.Stderr
	if err := dropCmd.Run(); err != nil {
		return fmt.Errorf("drop failed: %w", err)
	}
	slog.Info("tables dropped")

	// Step 2: Re-apply all migrations
	slog.Info("applying all migrations")
	upCmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("migration up failed: %w", err)
	}
	slog.Info("migrations applied")

	// Step 3: Seed
	if !skipSeed {
		slog.Info("seeding database")
		seedCmd := execCommand("go", "run", "./app/main", "seed")
		seedCmd.Stdout = os.Stdout
		seedCmd.Stderr = os.Stderr
		seedCmd.Stdin = os.Stdin
		if err := seedCmd.Run(); err != nil {
			return fmt.Errorf("seeding failed: %w", err)
		}
		slog.Info("seeding completed")
	}

	slog.Info("database reset complete")
	return nil
}
