package commands

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Drop all tables, re-migrate, and seed the database",
	Long: `Reset the database to a clean state by dropping all tables, re-applying
all migrations, and running seed functions.

Use --skip-seed to skip seeding after migration.`,
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
	dropCmd := exec.Command("migrate", "-path", "db/migrations", "-database", dbURL, "drop", "-f")
	dropCmd.Stdout = os.Stdout
	dropCmd.Stderr = os.Stderr
	if err := dropCmd.Run(); err != nil {
		return fmt.Errorf("drop failed: %w", err)
	}
	slog.Info("tables dropped")

	// Step 2: Re-apply all migrations
	slog.Info("applying all migrations")
	upCmd := exec.Command("migrate", "-path", "db/migrations", "-database", dbURL, "up")
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("migration up failed: %w", err)
	}
	slog.Info("migrations applied")

	// Step 3: Seed
	if !skipSeed {
		slog.Info("seeding database")
		seedCmd := exec.Command("go", "run", "./app/main", "seed")
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
