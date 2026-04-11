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
	Short: "Run the project in development mode with Air hot reload",
	Long: `Start the development loop against the current project on the host
machine (not inside Docker). The command does three things:

  1. Builds the migration URL from config.yaml and applies every pending
     migration via ` + "`migrate up`" + ` (skipped gracefully if the database is
     unreachable — useful before the DB container is up)
  2. Launches Air (` + "`go tool air`" + `) against the project's .air.toml, which
     rebuilds and restarts the binary on every source change
  3. Wires SIGINT/SIGTERM through to Air so Ctrl+C shuts down cleanly

Assumes the database is reachable — if you use the Docker dev loop,
start the DB first with ` + "`docker compose up db -d`" + `. If you want a fully
containerised dev loop, use ` + "`make up`" + ` instead (runs the app and DB in
Compose).

Prerequisites: Go toolchain, Air registered in go.mod (` + "`gofasta new`" + ` and
` + "`gofasta init`" + ` do this automatically), and ` + "`migrate`" + ` on $PATH if you want
auto-migration.`,
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
