package commands

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
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
	termcolor.PrintHeader("Starting gofasta development server...")

	// Load .env if present so config overrides (DATABASE_HOST, PORT, etc.)
	// propagate to both the migration preflight and the Air-spawned app.
	// Without this, a host-running app can't see the values users put in
	// .env and silently falls back to config.yaml defaults — which breaks
	// the "app on host, db in Docker" workflow.
	if loaded, err := loadDotEnv(".env"); err != nil {
		termcolor.PrintWarn(".env present but could not be loaded: %v", err)
	} else if loaded > 0 {
		termcolor.PrintStep("📋 Loaded %d variables from .env", loaded)
	}

	// Try running migrations. The database container may still be starting
	// (docker compose up db -d takes 1-3 seconds to accept connections), so
	// we retry once after a short pause before giving up. The error from the
	// migrate CLI is printed verbatim so the developer can see what actually
	// went wrong instead of guessing from a generic warning.
	termcolor.PrintStep("🗄  Running migrations...")
	if migErr := runMigrations(); migErr != nil {
		termcolor.PrintWarn("Migrations skipped: %v", migErr)
		termcolor.PrintHint("If the database is still starting, migrations will be applied on the next file save (Air rebuild).")
	}

	port := configutil.GetPort()
	fmt.Println()
	termcolor.PrintStep("🚀 Starting air (hot reload)...")
	fmt.Printf("   %s    %s\n", termcolor.CDim("REST API:"), termcolor.CBlue("http://localhost:"+port))
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		fmt.Printf("   %s     %s\n", termcolor.CDim("GraphQL:"), termcolor.CBlue("http://localhost:"+port+"/graphql"))
		fmt.Printf("   %s  %s\n", termcolor.CDim("Playground:"), termcolor.CBlue("http://localhost:"+port+"/graphql-playground"))
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

// runMigrations checks for the `migrate` CLI, builds the database URL from
// config, and applies pending migrations. If the first attempt fails (common
// when the database container is still starting), it waits briefly and
// retries once. Returns nil on success (including "no change"), or the
// underlying error on failure so the caller can print it verbatim.
func runMigrations() error {
	if _, err := execLookPath("migrate"); err != nil {
		return fmt.Errorf("migrate CLI not found on $PATH — install with:\n" +
			"  go install -tags 'postgres mysql sqlite3 sqlserver clickhouse' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1")
	}

	// configutil always builds a URL from defaults (at minimum
	// postgres://:@localhost:5432/?sslmode=disable), so a "" return is
	// not expected and not checked. If the URL is structurally wrong,
	// the migrate CLI will surface the error on the first attempt below.
	dbURL := configutil.BuildMigrationURL()

	// First attempt.
	if err := runMigrateUp(dbURL); err == nil {
		return nil
	}

	// Retry once after a short pause — gives the database container time
	// to finish accepting connections after `docker compose up db -d`.
	termcolor.PrintHint("Database not ready, retrying in 2 seconds...")
	time.Sleep(2 * time.Second)
	return runMigrateUp(dbURL)
}

func runMigrateUp(dbURL string) error {
	cmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
