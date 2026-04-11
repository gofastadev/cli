package commands

import (
	"fmt"
	"os"

	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the project — install deps, create .env, run migrations, verify setup",
	Long: `Set up a gofasta project for development. This command:
  1. Creates .env from .env.example (if .env doesn't exist)
  2. Runs go mod tidy
  3. Generates Wire DI code
  4. Generates GraphQL code (if gqlgen.yml exists)
  5. Runs database migrations
  6. Verifies the build compiles

Run this once after cloning the project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit()
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit() error {
	termcolor.PrintHeader("Initializing gofasta project...")

	// Step 1: Create .env if missing
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if _, err := os.Stat(".env.example"); err == nil {
			termcolor.PrintStep("📋 Creating .env from .env.example...")
			input, _ := os.ReadFile(".env.example")
			_ = os.WriteFile(".env", input, 0o644)
		} else {
			termcolor.PrintStep("📋 Creating empty .env file...")
			_ = os.WriteFile(".env", []byte("# Environment config\n"), 0o644)
		}
	} else {
		termcolor.PrintSuccess(".env already exists")
	}

	// Step 2: Install dependencies
	fmt.Println()
	termcolor.PrintStep("📦 Installing dependencies...")
	if err := runCmd("go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	// Step 3: Generate Wire DI
	fmt.Println()
	termcolor.PrintStep("🔌 Generating Wire DI code...")
	if err := runCmd("go", "tool", "wire", "./app/di/"); err != nil {
		termcolor.PrintWarn("Wire generation failed (you may need to fix compilation errors first)")
	}

	// Step 4: Generate GraphQL (only if project has GraphQL support)
	fmt.Println()
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		termcolor.PrintStep("📊 Generating GraphQL code...")
		if err := runCmd("go", "tool", "gqlgen", "generate"); err != nil {
			termcolor.PrintWarn("gqlgen generation failed (you may need to fix schema errors first)")
		}
	} else {
		termcolor.PrintStep("📊 Skipping GraphQL (no gqlgen.yml found)")
	}

	// Step 5: Run migrations
	fmt.Println()
	termcolor.PrintStep("🗄  Running database migrations...")
	dbURL := configutil.BuildMigrationURL()
	if dbURL != "" {
		migrateCmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
		migrateCmd.Stdout = os.Stdout
		migrateCmd.Stderr = os.Stderr
		if err := migrateCmd.Run(); err != nil {
			termcolor.PrintWarn("Migrations failed (is the database running?)")
			termcolor.PrintHint("Hint: run 'docker compose up db -d' to start the database")
		}
	} else {
		termcolor.PrintWarn("Could not load config (skipping migrations)")
	}

	// Step 6: Verify build
	fmt.Println()
	termcolor.PrintStep("🔨 Verifying build...")
	if err := runCmd("go", "build", "./..."); err != nil {
		return fmt.Errorf("build verification failed: %w", err)
	}

	fmt.Println()
	termcolor.PrintSuccess("Project initialized successfully!")
	// Reuse the same onboarding block that `gofasta new` prints so fresh
	// projects and cloned projects see identical next-steps. Pass an empty
	// name to skip the `cd` line — init runs from inside the project dir.
	printGetStarted("")
	return nil
}

// runCmd runs an external command and streams stdout/stderr to the user. The
// first argument is the program name; it is kept as a parameter (rather than
// hard-coded to "go") so tests and future callers can target other binaries.
//
//nolint:unparam // name parameter kept for future flexibility.
func runCmd(name string, args ...string) error {
	cmd := execCommand(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
