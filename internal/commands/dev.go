package commands

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Start development server with hot reload, auto-migration",
	Long: `Start the gofasta development server on your host machine.
This command:
  1. Runs database migrations (if DB is reachable)
  2. Starts air for hot reload
  3. Rebuilds on every file change

Prerequisites: Go installed, database running (use 'docker compose up db -d' for Docker DB)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDev()
	},
}

func init() {
	rootCmd.AddCommand(devCmd)
}

func runDev() error {
	fmt.Println("Starting gofasta development server...")

	// Try running migrations
	fmt.Println("🗄  Running migrations...")
	dbURL := configutil.BuildMigrationURL()
	if dbURL != "" {
		migrateCmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
		migrateCmd.Stdout = os.Stdout
		migrateCmd.Stderr = os.Stderr
		if err := migrateCmd.Run(); err != nil {
			fmt.Println("   ⚠ Migrations skipped (database may not be running)")
		}
	}

	port := configutil.GetPort()
	fmt.Println("\n🚀 Starting air (hot reload)...")
	fmt.Printf("   REST API:    http://localhost:%s\n", port)
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		fmt.Printf("   GraphQL:     http://localhost:%s/graphql\n", port)
		fmt.Printf("   Playground:  http://localhost:%s/graphql-playground\n", port)
	}
	fmt.Println()

	airCmd := execCommand("go", "tool", "air")
	airCmd.Stdout = os.Stdout
	airCmd.Stderr = os.Stderr
	airCmd.Stdin = os.Stdin

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if airCmd.Process != nil {
			_ = airCmd.Process.Signal(os.Interrupt)
		}
	}()

	return airCmd.Run()
}
