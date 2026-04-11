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
	Short: "Run database migrations",
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigration("up")
	},
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Rollback the last migration",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigration("down")
	},
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	rootCmd.AddCommand(migrateCmd)
}

func runMigration(direction string) error {
	dbURL := configutil.BuildMigrationURL()
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
