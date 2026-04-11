package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/gofastadev/cli/internal/skeleton"
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
	Use:   "new [project-name]",
	Short: "Create a new gofasta project",
	Long: `Bootstrap a new gofasta project from scratch. Creates the directory,
initializes Go modules, sets up the full project structure, and prepares
everything for development.

Examples:
  gofasta new myapp
  gofasta new github.com/myorg/myapp`,
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

	fmt.Printf("🚀 Creating new gofasta project: %s\n\n", projectName)

	// Create project directory
	fmt.Printf("📁 Creating directory %s/\n", projectDir)
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
	fmt.Printf("📦 Initializing Go module: %s\n", modulePath)
	if err := runCmdSilent("go", "mod", "init", modulePath); err != nil {
		return fmt.Errorf("go mod init failed: %w", err)
	}

	// Walk embedded skeleton and generate files
	fmt.Println("🏗  Creating project structure...")
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

		fmt.Printf("   %s\n", outputPath)
		return os.WriteFile(outputPath, output, 0o644)
	})
	if err != nil {
		return fmt.Errorf("generating project files: %w", err)
	}

	// Copy .env from .env.example
	if envExample, err := os.ReadFile(".env.example"); err == nil {
		_ = os.WriteFile(".env", envExample, 0o644)
		fmt.Println("   .env")
	}

	// Install gofasta library as a project dependency.
	fmt.Println("\n📦 Installing gofasta library...")
	if err := runCmdSilent("go", "get", "github.com/gofastadev/gofasta@latest"); err != nil {
		fmt.Println("   ⚠ Could not install gofasta library (you may need to add it manually)")
	}

	// Install cobra for project commands
	if err := runCmdSilent("go", "get", "github.com/spf13/cobra@latest"); err != nil {
		fmt.Println("   ⚠ Could not install cobra")
	}

	// Add tool dependencies
	fmt.Println("📦 Installing tool dependencies...")
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
	fmt.Println("📦 Running go mod tidy...")
	_ = runCmdSilent("go", "mod", "tidy")

	// Generate code
	fmt.Println("\n🔌 Generating Wire DI code...")
	if err := runCmdSilent("go", "tool", "wire", "./app/di/"); err != nil {
		fmt.Println("   ⚠ Wire generation skipped (can be run later with: make wire)")
	}

	if includeGraphQL {
		fmt.Println("📊 Generating GraphQL code...")
		if err := runCmdSilent("go", "tool", "gqlgen", "generate"); err != nil {
			fmt.Println("   ⚠ gqlgen generation skipped (can be run later with: make gqlgen)")
		}
	}

	// Initialize git
	fmt.Println("\n🔧 Initializing git repository...")
	_ = runCmdSilent("git", "init")
	_ = runCmdSilent("git", "add", ".")
	_ = runCmdSilent("git", "commit", "-m", "Initial commit: gofasta project scaffold")

	fmt.Printf("\n✅ Project %s created successfully!\n", projectName)
	fmt.Printf("\nGet started:\n")
	fmt.Printf("  cd %s\n", projectName)
	fmt.Printf("  make up                        # Start with Docker (recommended)\n")
	fmt.Printf("  # or\n")
	fmt.Printf("  docker compose up db -d        # Start DB only\n")
	fmt.Printf("  make dev                       # Run on host with hot reload\n")
	fmt.Printf("\nGenerate resources:\n")
	fmt.Printf("  gofasta g s Product name:string price:float\n")
	return nil
}

func runCmdSilent(name string, args ...string) error {
	cmd := execCommand(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
