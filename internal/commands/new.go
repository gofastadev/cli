package commands

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
		return runNew(args[0])
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
}

// dotfileRenames maps embedded names to actual dotfile names.
var dotfileRenames = map[string]string{
	"dot-env.example": ".env.example",
	"dot-env":         ".env",
	"dot-gitignore":   ".gitignore",
	"dot-go-version":  ".go-version",
	"air.toml":        ".air.toml",
}

func runNew(nameOrPath string) error {
	projectName := filepath.Base(nameOrPath)
	modulePath := nameOrPath
	if !strings.Contains(modulePath, "/") {
		modulePath = projectName
	}

	if _, err := os.Stat(projectName); err == nil {
		return fmt.Errorf("directory %q already exists", projectName)
	}

	data := ProjectData{
		ProjectName:      projectName,
		ProjectNameLower: strings.ToLower(projectName),
		ProjectNameUpper: strings.ToUpper(projectName),
		ModulePath:       modulePath,
	}

	fmt.Printf("🚀 Creating new gofasta project: %s\n\n", projectName)

	// Create project directory
	fmt.Printf("📁 Creating directory %s/\n", projectName)
	if err := os.MkdirAll(projectName, 0755); err != nil {
		return err
	}

	// Change into the new directory
	origDir, _ := os.Getwd()
	if err := os.Chdir(projectName); err != nil {
		return err
	}
	defer os.Chdir(origDir)

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

		if d.IsDir() {
			return os.MkdirAll(relPath, 0755)
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
			os.MkdirAll(dir, 0755)
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
		return os.WriteFile(outputPath, output, 0644)
	})
	if err != nil {
		return fmt.Errorf("generating project files: %w", err)
	}

	// Copy .env from .env.example
	if envExample, err := os.ReadFile(".env.example"); err == nil {
		os.WriteFile(".env", envExample, 0644)
		fmt.Println("   .env")
	}

	// Install gofasta framework as dependency
	fmt.Println("\n📦 Installing gofasta framework...")
	if err := runCmdSilent("go", "get", "github.com/gofastadev/gofasta@latest"); err != nil {
		fmt.Println("   ⚠ Could not install gofasta (you may need to add it manually)")
	}

	// Install cobra for project commands
	if err := runCmdSilent("go", "get", "github.com/spf13/cobra@latest"); err != nil {
		fmt.Println("   ⚠ Could not install cobra")
	}

	// Tidy
	fmt.Println("📦 Running go mod tidy...")
	runCmdSilent("go", "mod", "tidy")

	// Initialize git
	fmt.Println("\n🔧 Initializing git repository...")
	runCmdSilent("git", "init")
	runCmdSilent("git", "add", ".")
	runCmdSilent("git", "commit", "-m", "Initial commit: gofasta project scaffold")

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
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
