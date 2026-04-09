package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system prerequisites and project health",
	Long: `Verify that required and optional tools are installed, check project
configuration, and test database connectivity. Useful for diagnosing setup
issues and for including in bug reports.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor()
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

type doctorCheck struct {
	name     string
	required bool
	checkFn  func() (string, bool)
}

func runDoctor() error {
	allPassed := true

	required := []doctorCheck{
		{"go", true, checkGoVersion},
		{"migrate", true, checkMigrateVersion},
	}

	optional := []doctorCheck{
		{"docker", false, checkDockerVersion},
		{"air", false, checkGoTool("air")},
		{"wire", false, checkGoTool("wire")},
		{"gqlgen", false, checkGoTool("gqlgen")},
		{"swag", false, checkGoTool("swag")},
	}

	fmt.Println("Required:")
	for _, c := range required {
		info, ok := c.checkFn()
		printCheck(c.name, info, ok)
		if !ok {
			allPassed = false
		}
	}

	fmt.Println("\nOptional:")
	for _, c := range optional {
		info, ok := c.checkFn()
		printCheck(c.name, info, ok)
	}

	// Project health checks — only when inside a project directory
	if _, err := os.Stat("config.yaml"); err == nil {
		fmt.Println("\nProject:")
		printCheck("config.yaml", "found", true)

		dbURL := configutil.BuildMigrationURL()
		if dbURL != "" {
			cmd := exec.Command("migrate", "-path", "db/migrations", "-database", dbURL, "version")
			if err := cmd.Run(); err == nil {
				printCheck("database", "reachable", true)
			} else {
				printCheck("database", "not reachable", false)
			}
		}
	}

	if !allPassed {
		return fmt.Errorf("some required checks failed")
	}
	return nil
}

func printCheck(name, info string, ok bool) {
	mark := "\033[32m✓\033[0m"
	if !ok {
		mark = "\033[31m✗\033[0m"
	}
	fmt.Printf("  %s %-12s %s\n", mark, name, info)
}

func checkGoVersion() (string, bool) {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "not found", false
	}
	return strings.TrimSpace(string(out)), true
}

func checkMigrateVersion() (string, bool) {
	out, err := exec.Command("migrate", "--version").CombinedOutput()
	if err != nil {
		return "not found — install: https://github.com/golang-migrate/migrate", false
	}
	return strings.TrimSpace(string(out)), true
}

func checkDockerVersion() (string, bool) {
	out, err := exec.Command("docker", "--version").Output()
	if err != nil {
		return "not found (optional)", false
	}
	return strings.TrimSpace(string(out)), true
}

func checkGoTool(name string) func() (string, bool) {
	return func() (string, bool) {
		if err := exec.Command("go", "tool", "-n", name).Run(); err != nil {
			return fmt.Sprintf("not found — run: go get %s", goToolInstallHint(name)), false
		}
		return "available (Go tool)", true
	}
}

func goToolInstallHint(name string) string {
	hints := map[string]string{
		"air":    "github.com/air-verse/air@latest",
		"wire":   "github.com/google/wire/cmd/wire@latest",
		"gqlgen": "github.com/99designs/gqlgen@latest",
		"swag":   "github.com/swaggo/swag/cmd/swag@latest",
	}
	if hint, ok := hints[name]; ok {
		return hint
	}
	return name + "@latest"
}
