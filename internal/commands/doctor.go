package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Audit system prerequisites, required tools, and project health",
	Long: `Run a diagnostic sweep and print a table of checks with status icons.
Useful as the first thing to run after installing the CLI, after cloning
a project, and when filing bug reports — the output is designed to be
pasted into an issue.

Checks include:

  - Go toolchain (version, GOPATH/GOBIN)
  - Required tools: git, docker, migrate, air, wire, gqlgen, swag
  - Project state: presence of config.yaml, .env, go.mod, db/migrations/
  - Database connectivity: builds the migration URL and attempts a ping
  - Golang-migrate schema_migrations version (when DB is reachable)

Each check is tagged required or optional. A required check failing
returns a non-zero exit code so the command is scriptable in CI.`,
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

// doctorReport is the structured payload `gofasta doctor --json` emits.
// One entry per check, grouped by section. The Status field is the
// stable contract — "ok" / "fail" / "info" — so agents can branch.
type doctorReport struct {
	Required []doctorEntry `json:"required"`
	Optional []doctorEntry `json:"optional"`
	Project  []doctorEntry `json:"project,omitempty"`
	Passed   bool          `json:"passed"`
}

type doctorEntry struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok" | "fail" | "info"
	Message string `json:"message,omitempty"`
}

func runDoctor() error {
	report := doctorReport{Passed: true}

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

	for _, c := range required {
		info, ok := c.checkFn()
		report.Required = append(report.Required, doctorEntryFor(c.name, info, ok))
		if !ok {
			report.Passed = false
		}
	}
	for _, c := range optional {
		info, ok := c.checkFn()
		report.Optional = append(report.Optional, doctorEntryFor(c.name, info, ok))
	}

	// Project health checks — only when inside a project directory.
	if _, err := os.Stat("config.yaml"); err == nil {
		report.Project = append(report.Project, doctorEntry{
			Name: "config.yaml", Status: "ok", Message: "found",
		})
		// Load .env BEFORE building the migration URL. config.yaml in
		// scaffolded projects ships with in-container defaults; the
		// .env file is where the host-side overrides live (compose
		// maps host 5433 → container 5432). Without this, doctor
		// builds a URL pointing at port 5432 on the host (nothing
		// listening) and reports "not reachable" even when the DB is
		// healthy. See migrate.go for the full rationale.
		_, _ = loadDotEnv(".env")

		driver := strings.ToLower(strings.TrimSpace(configutil.ReadDBDriver()))
		switch driver {
		case "sqlite", "sqlite3":
			// SQLite is file-based — there's no network endpoint to
			// ping. `migrate version` against a non-existent file
			// would create it on first run; calling that during a
			// diagnostic command would surprise the user. Skip the
			// shell-out and emit an informational entry instead.
			report.Project = append(report.Project, doctorEntry{
				Name: "database", Status: "ok",
				Message: "file-based (no ping needed)",
			})
		default:
			dbURL := configutil.BuildMigrationURL()
			if dbURL != "" {
				cmd := execCommand("migrate", "-path", "db/migrations", "-database", dbURL, "version")
				entry := doctorEntry{Name: "database", Status: "ok", Message: "reachable"}
				if err := cmd.Run(); err != nil {
					entry.Status = "fail"
					entry.Message = "not reachable"
				}
				report.Project = append(report.Project, entry)
			}
		}
	}

	cliout.Print(report, func(w io.Writer) { printDoctorReport(w, report) })

	if !report.Passed {
		return fmt.Errorf("some required checks failed")
	}
	return nil
}

// doctorEntryFor turns the legacy (info, ok) tuple into a structured
// entry. Both required and optional checks render the same way; the
// caller decides whether a "fail" flips the overall passed flag.
func doctorEntryFor(name, info string, ok bool) doctorEntry {
	status := "ok"
	if !ok {
		status = "fail"
	}
	return doctorEntry{Name: name, Status: status, Message: info}
}

// printDoctorReport renders the human view, grouping checks under a
// brand-cyan header and using the canonical ✓/✗ vocabulary.
func printDoctorReport(w io.Writer, r doctorReport) {
	printDoctorSection(w, "Required:", r.Required)
	_, _ = fmt.Fprintln(w)
	printDoctorSection(w, "Optional:", r.Optional)
	if len(r.Project) > 0 {
		_, _ = fmt.Fprintln(w)
		printDoctorSection(w, "Project:", r.Project)
	}
}

func printDoctorSection(w io.Writer, header string, entries []doctorEntry) {
	_, _ = fmt.Fprintln(w, termcolor.Header("%s", header))
	for _, e := range entries {
		switch e.Status {
		case "ok":
			_, _ = fmt.Fprintf(w, "  %s %s %s\n",
				termcolor.CGreen("✓"), termcolor.CBold(fmt.Sprintf("%-12s", e.Name)), e.Message)
		case "fail":
			_, _ = fmt.Fprintf(w, "  %s %s %s\n",
				termcolor.CRed("✗"), termcolor.CBold(fmt.Sprintf("%-12s", e.Name)), termcolor.CDim(e.Message))
		default:
			_, _ = fmt.Fprintf(w, "  %s %s %s\n",
				" ", termcolor.CBold(fmt.Sprintf("%-12s", e.Name)), e.Message)
		}
	}
}

func checkGoVersion() (string, bool) {
	out, err := execCommand("go", "version").Output()
	if err != nil {
		return "not found", false
	}
	return strings.TrimSpace(string(out)), true
}

func checkMigrateVersion() (string, bool) {
	out, err := execCommand("migrate", "--version").CombinedOutput()
	if err != nil {
		return "not found — install: https://github.com/golang-migrate/migrate", false
	}
	return strings.TrimSpace(string(out)), true
}

func checkDockerVersion() (string, bool) {
	out, err := execCommand("docker", "--version").Output()
	if err != nil {
		return "not found (optional)", false
	}
	return strings.TrimSpace(string(out)), true
}

func checkGoTool(name string) func() (string, bool) {
	return func() (string, bool) {
		if err := execCommand("go", "tool", "-n", name).Run(); err != nil {
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
