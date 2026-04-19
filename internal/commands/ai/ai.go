package ai

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
)

// fprintln / fprintf are local helpers that swallow the write errors
// fmt.Fprint* return. Progress output is fire-and-forget — if the writer
// has gone away there is nothing actionable to do — and errcheck would
// otherwise flag every call site. Mirrors the pattern in root.go.
func fprintln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}

func fprintf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

// Cmd is the root `gofasta ai` command exported so the commands package
// can register it on the top-level rootCmd. Subcommands: <agent>, list,
// status.
var Cmd = &cobra.Command{
	Use:   "ai <agent>",
	Short: "Install AI coding agent configuration into the current project",
	Long: `Install the project-specific configuration files an AI coding agent
needs to work smoothly in this codebase — permission allowlists, hooks,
conventions files, and slash commands.

Ships only AGENTS.md by default (the universal file every modern agent
reads); per-agent configuration is opt-in via this command so developers
who don't use AI agents aren't cluttered with dotfiles they don't need.

Every installer is idempotent — re-running after a gofasta update
refreshes the config without touching files you've edited.

Examples:
  gofasta ai list              # Show every supported agent
  gofasta ai status            # Show which agents are installed in this project
  gofasta ai claude            # Install Claude Code config
  gofasta ai cursor --dry-run  # Preview what Cursor would install
  gofasta ai aider --force     # Overwrite existing Aider config`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runInstall(args[0], installDryRun, installForce)
	},
}

var (
	installDryRun bool
	installForce  bool
)

// listCmd lists every supported agent.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List every AI agent supported by `gofasta ai`",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList()
	},
}

// statusCmd shows which agents are currently installed in this project.
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which AI agents have configuration installed in this project",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func init() {
	Cmd.Flags().BoolVar(&installDryRun, "dry-run", false,
		"Preview what would be written without touching disk")
	Cmd.Flags().BoolVar(&installForce, "force", false,
		"Overwrite existing files whose contents differ from the template")
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(statusCmd)
}

// runInstall is the entry point for `gofasta ai <agent>`. Resolves the
// agent, verifies we're in a gofasta project, reads go.mod for the
// module path, renders templates, updates the manifest.
func runInstall(key string, dryRun, force bool) error {
	agent := AgentByKey(key)
	if agent == nil {
		return clierr.Newf(clierr.CodeUnknownAgent,
			"unknown agent %q — run `gofasta ai list` to see supported agents", key)
	}

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	data, err := buildInstallData(root)
	if err != nil {
		return err
	}

	result, err := Install(agent, root, data, InstallOptions{
		DryRun: dryRun,
		Force:  force,
	})
	if err != nil {
		return err
	}

	// Update manifest on successful non-dry-run install.
	if !dryRun {
		m, err := LoadManifest(root)
		if err != nil {
			return err
		}
		m.RecordInstall(agent.Key, data.CLIVersion)
		if err := m.Save(root); err != nil {
			return err
		}
	}

	// Render result — JSON payload or a human summary.
	cliout.Print(result, func(w io.Writer) {
		if dryRun {
			fprintf(w, "Dry run: %s would be installed into %s\n", agent.Name, root)
		} else {
			fprintf(w, "%s installed into %s\n", agent.Name, root)
		}
		result.PrintText(w)
		printNextSteps(w, agent)
	})
	return nil
}

// runList prints every supported agent. In JSON mode emits the full
// Agents slice so automation can enumerate programmatically.
func runList() error {
	cliout.Print(Agents, func(w io.Writer) {
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fprintln(tw, "KEY\tNAME\tDESCRIPTION")
		for _, a := range Agents {
			fprintf(tw, "%s\t%s\t%s\n", a.Key, a.Name, a.Description)
		}
		_ = tw.Flush()
	})
	return nil
}

// runStatus reports installed agents + when they were installed.
func runStatus() error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	m, err := LoadManifest(root)
	if err != nil {
		return err
	}

	type statusRow struct {
		Agent       string `json:"agent"`
		Name        string `json:"name"`
		InstalledAt string `json:"installed_at"`
		CLIVersion  string `json:"cli_version"`
	}
	rows := make([]statusRow, 0, len(m.Installed))
	for _, key := range m.InstalledKeys() {
		rec := m.Installed[key]
		name := key
		if a := AgentByKey(key); a != nil {
			name = a.Name
		}
		rows = append(rows, statusRow{
			Agent:       key,
			Name:        name,
			InstalledAt: rec.InstalledAt.Format("2006-01-02 15:04 UTC"),
			CLIVersion:  rec.CLIVersion,
		})
	}

	cliout.Print(rows, func(w io.Writer) {
		if len(rows) == 0 {
			fprintln(w, "No AI agents installed in this project.")
			fprintln(w, "Run `gofasta ai list` to see supported agents.")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fprintln(tw, "AGENT\tNAME\tINSTALLED AT\tCLI VERSION")
		for _, r := range rows {
			fprintf(tw, "%s\t%s\t%s\t%s\n", r.Agent, r.Name, r.InstalledAt, r.CLIVersion)
		}
		_ = tw.Flush()
	})
	return nil
}

// printNextSteps emits an agent-specific hint after a successful install
// so the user knows what to do next.
func printNextSteps(w io.Writer, agent *Agent) {
	fprintln(w)
	fprintln(w, "Next steps:")
	switch agent.Key {
	case "claude":
		fprintln(w, "  Open this project in Claude Code. The following commands are pre-approved:")
		fprintln(w, "    gofasta *, make *, go build/test/vet, gofmt, common read-only git")
		fprintln(w, "  Slash commands available: /verify, /scaffold, /inspect")
	case "cursor":
		fprintln(w, "  Open this project in Cursor. `.cursor/rules/gofasta.mdc` will apply to every edit.")
	case "codex":
		fprintln(w, "  Point OpenAI Codex at this project root. It will read AGENTS.md + .codex/config.toml.")
	case "aider":
		fprintln(w, "  Start Aider: `aider` from the project root. Auto-test + auto-lint are enabled.")
	case "windsurf":
		fprintln(w, "  Open this project in Windsurf. `.windsurfrules` applies to every edit.")
	}
}

// findProjectRoot walks up from the current directory looking for a go.mod
// file. Returns the absolute path to the directory containing go.mod, or
// a clierr if not inside a Go module.
func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", clierr.Wrap(clierr.CodeFileIO, err, "could not determine current directory")
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", clierr.New(clierr.CodeNotGofastaProject,
				"not inside a Go module (no go.mod found in parent directories)")
		}
		dir = parent
	}
}

// buildInstallData reads go.mod to extract the module path, then derives
// the various name variants templates use.
func buildInstallData(projectRoot string) (InstallData, error) {
	content, err := os.ReadFile(filepath.Join(projectRoot, "go.mod"))
	if err != nil {
		return InstallData{}, clierr.Wrap(clierr.CodeFileIO, err,
			"could not read go.mod")
	}
	modulePath := extractModulePath(string(content))
	name := moduleName(modulePath)

	cliVersion := "dev"
	if root := rootCmdVersion(); root != "" {
		cliVersion = root
	}

	return InstallData{
		ProjectName:      name,
		ProjectNameLower: strings.ToLower(name),
		ProjectNameUpper: strings.ToUpper(name),
		ModulePath:       modulePath,
		CLIVersion:       cliVersion,
	}, nil
}

// extractModulePath reads the `module <path>` line out of a go.mod. Kept
// local to the ai package so we don't add a go.mod-parsing dep.
func extractModulePath(goMod string) string {
	for _, line := range strings.Split(goMod, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// moduleName returns the last segment of a module path ("github.com/org/app" → "app").
func moduleName(modulePath string) string {
	if modulePath == "" {
		return ""
	}
	parts := strings.Split(modulePath, "/")
	return parts[len(parts)-1]
}

// rootCmdVersion reaches up to the parent `commands` package's rootCmd
// for its Version string. Implemented as a function variable so the
// commands package can set it during its init without an import cycle
// between commands → ai → commands.
var rootCmdVersion = func() string { return "" }

// SetVersionResolver is called from the commands package at init time
// so runInstall can stamp the manifest with the actual CLI version
// instead of "dev".
func SetVersionResolver(fn func() string) {
	if fn != nil {
		rootCmdVersion = fn
	}
}
