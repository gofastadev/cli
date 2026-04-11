package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/gofastadev/cli/internal/skeleton"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// ProjectData holds template variables for project generation.
type ProjectData struct {
	ProjectName      string // PascalCase or as-provided: "MyApp"
	ProjectNameLower string // lowercase: "myapp"
	ProjectNameUpper string // UPPERCASE: "MYAPP"
	ModulePath       string // Go module path: "github.com/myorg/myapp"
	GraphQL          bool   // true when --graphql flag is passed
}

// graphqlOnlyPaths lists skeleton paths that should be skipped for REST-only projects.
var graphqlOnlyPaths = []string{
	"app/graphql/",
	"app/generated_stub.go.tmpl",
	"app/di/providers/graphql.go.tmpl",
	"gqlgen.yml.tmpl",
}

var newCmd = &cobra.Command{
	Use:   "new [name-or-module-path]",
	Short: "Scaffold a complete, ready-to-run Go backend project",
	Long: `Create a new gofasta project from the embedded template. Generates ~78 files
covering models, services, repositories, REST controllers, routes, DTOs,
database migrations, Docker/Compose setup, CI configs, Wire dependency
injection, and a Makefile — the full production layout described in the
project README.

The single argument is either a bare project name (used as the directory
name and module path) or a fully-qualified Go module path. When given a
module path, the directory is named after the last segment:

  gofasta new myapp                      # module: myapp, dir: myapp/
  gofasta new github.com/acme/myapp      # module: github.com/acme/myapp, dir: myapp/

What the command does, in order:
  1. Creates the project directory (fails if it already exists)
  2. Runs ` + "`go mod init`" + ` with the resolved module path
  3. Renders every template file from the embedded skeleton, replacing
     {{.ModulePath}} / {{.ProjectNameLower}} / {{.ProjectNameUpper}}
  4. Copies .env from the generated .env.example
  5. Runs ` + "`go get`" + ` for github.com/gofastadev/gofasta and the tool deps
     (Wire, Air, swag — and gqlgen if --graphql is set)
  6. Registers those tools via ` + "`go mod edit -tool`" + ` so ` + "`go tool wire`" + ` works
  7. Runs ` + "`go mod tidy`" + `
  8. Generates Wire DI code and (if --graphql) the gqlgen resolver stubs
  9. Initializes a git repository with an initial commit

Use --graphql (or the shorthand --gql) to additionally scaffold an
app/graphql/ directory with gqlgen, a GraphQL provider, and the generated
resolver stubs. Without the flag, the project is REST-only.

After the command finishes, ` + "`cd`" + ` into the new directory and run
` + "`make up`" + ` (Docker-based dev loop) or ` + "`docker compose up db -d && make dev`" + `
(host-based with hot reload via Air).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		gql, _ := cmd.Flags().GetBool("graphql")
		gqlShort, _ := cmd.Flags().GetBool("gql")
		return runNew(args[0], gql || gqlShort)
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().Bool("graphql", false, "Include GraphQL support (gqlgen) alongside REST")
	newCmd.Flags().Bool("gql", false, "Shorthand for --graphql")
}

// dotfileRenames maps embedded names to actual dotfile names.
var dotfileRenames = map[string]string{
	"dot-env.example": ".env.example",
	"dot-env":         ".env",
	"dot-gitignore":   ".gitignore",
	"dot-go-version":  ".go-version",
	"air.toml":        ".air.toml",
}

// resolveProjectPaths extracts the project directory, display name, and Go module path
// from the user-provided nameOrPath argument.
func resolveProjectPaths(nameOrPath string) (projectDir, projectName, modulePath string) {
	projectName = filepath.Base(nameOrPath)

	// Use full path for absolute paths, otherwise just the base name.
	projectDir = nameOrPath
	if !filepath.IsAbs(nameOrPath) {
		projectDir = projectName
	}

	// Module path: use nameOrPath only if it looks like a Go module path
	// (e.g. github.com/org/repo), not a filesystem path.
	modulePath = projectName
	if strings.Contains(nameOrPath, "/") && !filepath.IsAbs(nameOrPath) {
		modulePath = nameOrPath
	}
	return
}

//nolint:gocognit,gocyclo // linear scaffold pipeline; refactoring would obscure the flow.
func runNew(nameOrPath string, includeGraphQL bool) error {
	projectDir, projectName, modulePath := resolveProjectPaths(nameOrPath)

	if _, err := os.Stat(projectDir); err == nil {
		return fmt.Errorf("directory %q already exists", projectDir)
	}

	data := ProjectData{
		ProjectName:      projectName,
		ProjectNameLower: strings.ToLower(projectName),
		ProjectNameUpper: strings.ToUpper(projectName),
		ModulePath:       modulePath,
		GraphQL:          includeGraphQL,
	}

	termcolor.PrintHeader("🚀 Creating new gofasta project: %s", projectName)
	fmt.Println()

	// Create project directory
	termcolor.PrintStep("📁 Creating directory %s/", projectDir)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}

	// Change into the new directory
	origDir, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		return err
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Initialize go module
	termcolor.PrintStep("📦 Initializing Go module: %s", modulePath)
	if err := runCmdSilent("go", "mod", "init", modulePath); err != nil {
		return fmt.Errorf("go mod init failed: %w", err)
	}

	// Walk embedded skeleton and generate files
	termcolor.PrintStep("🏗  Creating project structure...")
	projectFS := skeleton.ProjectFS
	err := fs.WalkDir(projectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the "project/" prefix to get the relative output path
		relPath := strings.TrimPrefix(path, "project/")
		if relPath == "" || relPath == "project" {
			return nil
		}

		// Skip GraphQL-only files when not using --graphql
		if !data.GraphQL {
			for _, prefix := range graphqlOnlyPaths {
				if strings.HasPrefix(relPath, prefix) {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
			}
		}

		if d.IsDir() {
			return os.MkdirAll(relPath, 0o755)
		}

		// Read the embedded file
		content, err := fs.ReadFile(projectFS, path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		// Determine output path and whether to template
		outputPath := relPath
		isTemplate := strings.HasSuffix(outputPath, ".tmpl")
		if isTemplate {
			outputPath = strings.TrimSuffix(outputPath, ".tmpl")
		}

		// Handle dotfile renames
		base := filepath.Base(outputPath)
		if renamed, ok := dotfileRenames[base]; ok {
			outputPath = filepath.Join(filepath.Dir(outputPath), renamed)
		}

		// Ensure parent directory exists
		if dir := filepath.Dir(outputPath); dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}

		var output []byte
		if isTemplate {
			tmpl, err := template.New(filepath.Base(path)).Parse(string(content))
			if err != nil {
				return fmt.Errorf("parsing template %s: %w", path, err)
			}
			var buf strings.Builder
			if err := tmpl.Execute(&buf, data); err != nil {
				return fmt.Errorf("executing template %s: %w", path, err)
			}
			output = []byte(buf.String())
		} else {
			output = content
		}

		termcolor.PrintPath(outputPath)
		return os.WriteFile(outputPath, output, 0o644)
	})
	if err != nil {
		return fmt.Errorf("generating project files: %w", err)
	}

	// Copy .env from .env.example
	if envExample, err := os.ReadFile(".env.example"); err == nil {
		_ = os.WriteFile(".env", envExample, 0o644)
		termcolor.PrintPath(".env")
	}

	// Install gofasta library as a project dependency.
	fmt.Println()
	termcolor.PrintStep("📦 Installing gofasta library...")
	if err := runCmdSilent("go", "get", "github.com/gofastadev/gofasta@latest"); err != nil {
		termcolor.PrintWarn("Could not install gofasta library (you may need to add it manually)")
	}

	// Install cobra for project commands
	if err := runCmdSilent("go", "get", "github.com/spf13/cobra@latest"); err != nil {
		termcolor.PrintWarn("Could not install cobra")
	}

	// Add tool dependencies
	termcolor.PrintStep("📦 Installing tool dependencies...")
	if includeGraphQL {
		_ = runCmdSilent("go", "get", "github.com/99designs/gqlgen@latest")
	}
	_ = runCmdSilent("go", "get", "github.com/google/wire/cmd/wire@latest")
	_ = runCmdSilent("go", "get", "github.com/air-verse/air@latest")
	_ = runCmdSilent("go", "get", "github.com/swaggo/swag/cmd/swag@latest")
	// Register as Go tools
	if includeGraphQL {
		_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/99designs/gqlgen")
	}
	_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/google/wire/cmd/wire")
	_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/air-verse/air")
	_ = runCmdSilent("go", "mod", "edit", "-tool", "github.com/swaggo/swag/cmd/swag")

	// Tidy
	termcolor.PrintStep("📦 Running go mod tidy...")
	_ = runCmdSilent("go", "mod", "tidy")

	// Generate code
	fmt.Println()
	termcolor.PrintStep("🔌 Generating Wire DI code...")
	if err := runCmdSilent("go", "tool", "wire", "./app/di/"); err != nil {
		termcolor.PrintWarn("Wire generation skipped (can be run later with: make wire)")
	}

	if includeGraphQL {
		termcolor.PrintStep("📊 Generating GraphQL code...")
		if err := runCmdSilent("go", "tool", "gqlgen", "generate"); err != nil {
			termcolor.PrintWarn("gqlgen generation skipped (can be run later with: make gqlgen)")
		}
	}

	// Initialize git
	fmt.Println()
	termcolor.PrintStep("🔧 Initializing git repository...")
	_ = runCmdSilent("git", "init")
	_ = runCmdSilent("git", "add", ".")
	_ = runCmdSilent("git", "commit", "-m", "Initial commit: gofasta project scaffold")

	fmt.Println()
	termcolor.PrintSuccess("Project %s created successfully!", termcolor.CBold(projectName))
	printGetStarted(projectName)
	return nil
}

// printGetStarted renders the post-scaffold onboarding block. Extracted so
// `gofasta init` can reuse the exact same messaging — developers should see
// the same next-steps regardless of whether they created a fresh project or
// cloned an existing one. Pass an empty projectName to skip the `cd` line
// (useful for init, which runs from inside the project directory).
func printGetStarted(projectName string) {
	fmt.Println()
	termcolor.PrintHeader("Next steps:")
	fmt.Println()
	if projectName != "" {
		fmt.Printf("  %s\n", termcolor.CBold("cd "+projectName))
		fmt.Println()
	}

	// --- Development workflows ---------------------------------------------
	//
	// Three workflows, most-containerized first. Each is labeled with its
	// tradeoff so the developer can pick without having to read the docs.
	// gofasta commands are shown first; `make` targets are demoted to an
	// "Also available as" block at the bottom.
	termcolor.PrintHeader("Pick a development workflow:")
	fmt.Println()

	fmt.Printf("  %s  %s%s\n",
		termcolor.CBold("A."),
		termcolor.CBold("Everything in Docker"),
		termcolor.CDim(" — fully containerized, zero host setup"))
	fmt.Printf("     %s   %s\n", termcolor.CBold("docker compose up -d        "), termcolor.CDim("# build + start app and db"))
	fmt.Printf("     %s   %s\n", termcolor.CBold("docker compose logs -f app  "), termcolor.CDim("# tail application logs"))
	fmt.Printf("     %s   %s\n", termcolor.CBold("docker compose down         "), termcolor.CDim("# stop everything"))
	fmt.Println()

	fmt.Printf("  %s  %s%s\n",
		termcolor.CBold("B."),
		termcolor.CBold("App on host, db in Docker"),
		termcolor.CDim(" — fastest iteration, Air hot reload"))
	fmt.Printf("     %s   %s\n", termcolor.CBold("docker compose up db -d     "), termcolor.CDim("# start only the database"))
	fmt.Printf("     %s   %s\n", termcolor.CBold("gofasta dev                 "), termcolor.CDim("# run app with hot reload + auto-migrate"))
	fmt.Println()

	fmt.Printf("  %s  %s%s\n",
		termcolor.CBold("C."),
		termcolor.CBold("Everything on host"),
		termcolor.CDim(" — you manage your own database"))
	fmt.Printf("     %s   %s\n", termcolor.CBold("gofasta dev                 "), termcolor.CDim("# expects db at the address in config.yaml"))
	fmt.Println()

	// --- Common tasks -------------------------------------------------------
	termcolor.PrintHeader("Common tasks:")
	fmt.Println()
	tasks := [][2]string{
		{"gofasta g scaffold Product name:string price:float", "generate a full REST resource, auto-wired end-to-end"},
		{"gofasta g model Product name:string price:float", "just the model + matching migration"},
		{"gofasta g job cleanup-tokens \"0 0 0 * * *\"", "scheduled cron job"},
		{"gofasta g task send-welcome-email", "async background task for the queue worker"},
		{"gofasta migrate up", "apply all pending database migrations"},
		{"gofasta migrate down", "roll back the most recent migration"},
		{"gofasta seed", "run seed functions (--fresh drops + re-migrates first)"},
		{"gofasta routes", "list every registered REST route"},
		{"gofasta swagger", "regenerate OpenAPI docs from code annotations"},
		{"gofasta doctor", "check prerequisites and project health"},
	}
	for _, ln := range tasks {
		fmt.Printf("  %-55s %s\n", termcolor.CBold(ln[0]), termcolor.CDim("# "+ln[1]))
	}
	fmt.Println()

	// --- Make shortcuts (thin wrappers over the gofasta commands above) ---
	termcolor.PrintHeader("Also available as Make targets:")
	fmt.Println()
	makeShortcuts := [][2]string{
		{"make up", "docker compose up -d"},
		{"make down", "docker compose down"},
		{"make dev", "gofasta dev"},
		{"make migrate", "gofasta migrate up"},
		{"make seed", "gofasta seed"},
	}
	for _, ln := range makeShortcuts {
		fmt.Printf("  %-14s %s\n", termcolor.CBold(ln[0]), termcolor.CDim("→ "+ln[1]))
	}
	fmt.Println()

	// --- Where to go next ---------------------------------------------------
	termcolor.PrintHeader("Full command reference:")
	fmt.Println()
	fmt.Printf("  %s            %s\n", termcolor.CBold("gofasta --help           "), termcolor.CDim("# every command, grouped by purpose"))
	fmt.Printf("  %s            %s\n", termcolor.CBold("gofasta <command> --help "), termcolor.CDim("# details for a specific command"))
	fmt.Println()
}

func runCmdSilent(name string, args ...string) error {
	cmd := execCommand(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
