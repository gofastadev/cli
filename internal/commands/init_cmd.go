package commands

import (
	"fmt"
	"os"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
)

// initResult is the JSON contract for `gofasta init --json`. Each step
// is recorded with its status (ok / warn / fail / skip) so an agent can
// pattern-match on the exact step that needs intervention rather than
// regex-scanning English text.
type initResult struct {
	Action  string     `json:"action"`
	Steps   []initStep `json:"steps"`
	Success bool       `json:"success"`
	Error   string     `json:"error,omitempty"`
}

type initStep struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail | skip
	Error  string `json:"error,omitempty"`
}

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
	steps := initSteps{}

	if !cliout.JSON() {
		cliout.Header("Initializing gofasta project...")
	}

	// Step 1: Create .env if missing
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if _, err := os.Stat(".env.example"); err == nil {
			cliout.Step("Creating .env from .env.example")
			input, _ := os.ReadFile(".env.example")
			_ = os.WriteFile(".env", input, 0o644)
			steps.add("env.create", "ok", nil)
		} else {
			cliout.Step("Creating empty .env file")
			_ = os.WriteFile(".env", []byte("# Environment config\n"), 0o644)
			steps.add("env.create", "ok", nil)
		}
	} else {
		cliout.Success(".env already exists")
		steps.add("env.create", "skip", nil)
	}

	cliout.Blank()
	cliout.Step("Installing dependencies")
	if err := runCmd("go", "mod", "tidy"); err != nil {
		steps.add("go.mod.tidy", "fail", err)
		return finishInit(steps, fmt.Errorf("go mod tidy failed: %w", err))
	}
	steps.add("go.mod.tidy", "ok", nil)

	cliout.Blank()
	cliout.Step("Generating Wire DI code")
	if err := runCmd("go", "tool", "wire", "./app/di/"); err != nil {
		cliout.Warn("Wire generation failed (you may need to fix compilation errors first)")
		steps.add("wire", "warn", err)
	} else {
		steps.add("wire", "ok", nil)
	}

	cliout.Blank()
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		cliout.Step("Generating GraphQL code")
		if err := runCmd("go", "tool", "gqlgen", "generate"); err != nil {
			cliout.Warn("gqlgen generation failed (you may need to fix schema errors first)")
			steps.add("gqlgen", "warn", err)
		} else {
			steps.add("gqlgen", "ok", nil)
		}
	} else {
		cliout.Step("Skipping GraphQL (no gqlgen.yml found)")
		steps.add("gqlgen", "skip", nil)
	}

	cliout.Blank()
	cliout.Step("Generating Swagger/OpenAPI docs")
	if err := runCmd("go", "tool", "swag", "init",
		"-g", "app/main/main.go", "-o", "docs/",
		"--parseDependency", "--parseInternal"); err != nil {
		cliout.Warn("Swagger generation skipped (can be run later with: gofasta swagger)")
		steps.add("swagger", "warn", err)
	} else {
		steps.add("swagger", "ok", nil)
	}

	cliout.Blank()
	cliout.Step("Running database migrations")
	// Load the .env we just created (or already had) so the migrate URL
	// includes user/password/name and the host-side port mapping. See
	// migrate.go for the full why — config.yaml in scaffolded projects
	// intentionally omits credentials, and `migrate` would otherwise
	// dial the wrong port with empty creds.
	_, _ = loadDotEnv(".env")
	dbURL := buildMigrationURL()
	if dbURL != "" {
		migrateCmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "up")
		// Migrate stdout streams to stderr in JSON mode so it doesn't
		// pollute the structured result we emit at the end. In text
		// mode it still goes to the user's terminal as before.
		if cliout.JSON() {
			migrateCmd.Stdout = os.Stderr
		} else {
			migrateCmd.Stdout = os.Stdout
		}
		migrateCmd.Stderr = os.Stderr
		if err := migrateCmd.Run(); err != nil {
			cliout.Warn("Migrations failed (is the database running?)")
			cliout.Hint("Hint: run 'docker compose up db -d' to start the database")
			steps.add("migrate", "warn", err)
		} else {
			steps.add("migrate", "ok", nil)
		}
	} else {
		cliout.Warn("Could not load config (skipping migrations)")
		steps.add("migrate", "skip", nil)
	}

	cliout.Blank()
	cliout.Step("Verifying build")
	if err := runCmd("go", "build", "./..."); err != nil {
		steps.add("build", "fail", err)
		return finishInit(steps, fmt.Errorf("build verification failed: %w", err))
	}
	steps.add("build", "ok", nil)

	if !cliout.JSON() {
		cliout.Blank()
		cliout.Success("Project initialized successfully!")
		// Reuse the same onboarding block that `gofasta new` prints so fresh
		// projects and cloned projects see identical next-steps. Pass an empty
		// name to skip the `cd` line — init runs from inside the project dir.
		printGetStarted("")
	}
	return finishInit(steps, nil)
}

// initSteps collects per-step outcomes for the JSON contract while also
// providing thin wrappers around the termcolor printers so the same
// call site populates both views without sprinkling cliout.JSON()
// checks everywhere.
type initSteps []initStep

func (s *initSteps) add(name, status string, err error) {
	step := initStep{Name: name, Status: status}
	if err != nil {
		step.Error = err.Error()
	}
	*s = append(*s, step)
}

// finishInit emits the final structured result in JSON mode and
// returns the original error so the root error handler still sets a
// non-zero exit code on failure.
func finishInit(steps initSteps, err error) error {
	if cliout.JSON() {
		result := initResult{
			Action:  "init",
			Steps:   steps,
			Success: err == nil,
		}
		if err != nil {
			result.Error = err.Error()
		}
		cliout.Print(result, nil)
	}
	return err
}

// runCmd runs an external command. In text mode stdout/stderr stream to
// the user's terminal (live progress). In JSON mode stdout is rerouted
// to stderr so the structured result we emit at the end is the only
// thing on stdout. The first argument is the program name; kept as a
// parameter so tests and future callers can target other binaries.
func runCmd(name string, args ...string) error {
	cmd := execCommand(name, args...)
	if cliout.JSON() {
		cmd.Stdout = os.Stderr
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
