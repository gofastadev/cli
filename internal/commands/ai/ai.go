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
	"github.com/gofastadev/cli/internal/termcolor"
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
	installSwitch bool
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
	Cmd.Flags().BoolVar(&installSwitch, "switch", false,
		"Replace the currently-installed agent with this one (uninstalls the previous agent first)")
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(statusCmd)
}

// runInstall is the entry point for `gofasta ai <agent>`. Resolves the
// agent, verifies we're in a gofasta project, reads go.mod for the
// module path, renders templates, updates the manifest.
//
// Single-active-agent invariant: if the manifest records a different
// active agent, runInstall refuses unless `--switch` is passed. With
// `--switch`, the previous agent is uninstalled (its files removed,
// its rename reversed) before the new agent is installed.
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

	m, err := LoadManifest(root)
	if err != nil {
		return err
	}

	// Single-active-agent guard. A different agent is installed:
	// without --switch, refuse and print the would-be diff so the user
	// can decide. With --switch, uninstall the previous agent first.
	if m.ActiveAgent != "" && m.ActiveAgent != agent.Key {
		if !installSwitch {
			return agentConflictError(m, agent, root, data)
		}
		if err := switchUninstall(m, root, dryRun); err != nil {
			return err
		}
	}

	result, err := Install(agent, root, data, InstallOptions{
		DryRun: dryRun,
		Force:  force,
	})
	if err != nil {
		return err
	}

	// Update manifest on successful non-dry-run install. Record the
	// full template path list so uninstall can reverse the install
	// without re-walking templates (which may shift between CLI versions).
	if !dryRun {
		ownedFiles, ferr := agentOwnedFiles(agent)
		if ferr != nil {
			return ferr
		}
		m.RecordInstall(agent.Key, data.CLIVersion, ownedFiles)
		if err := m.Save(root); err != nil {
			return err
		}
	}

	// Render result — JSON payload or a human summary.
	cliout.Print(result, func(w io.Writer) {
		if dryRun {
			fprintln(w, termcolor.Step("Dry run: %s would be installed into %s", agent.Name, root))
		} else {
			fprintln(w, termcolor.Success("%s installed into %s", agent.Name, root))
		}
		result.PrintText(w)
		printNextSteps(w, agent)
	})
	return nil
}

// agentOwnedFiles returns every project-relative path the agent's
// templates render to. Stamped into the manifest at install time so
// uninstall can reverse the install precisely.
func agentOwnedFiles(agent *Agent) ([]string, error) {
	files, err := TemplateFiles(agent)
	if err != nil {
		return nil, clierr.Wrap(clierr.CodeAIInstallFailed, err,
			"could not enumerate templates for agent "+agent.Key)
	}
	out := make([]string, 0, len(files))
	for _, tf := range files {
		out = append(out, tf.DestPath)
	}
	return out, nil
}

// agentConflictError builds the "another agent is installed" error,
// inlining the would-be diff (a dry-run of both the uninstall and the
// install) so the user can see exactly what `--switch` would do.
func agentConflictError(m *Manifest, target *Agent, root string, data InstallData) error {
	prev := AgentByKey(m.ActiveAgent)
	prevName := m.ActiveAgent
	if prev != nil {
		prevName = prev.Name
	}

	var diff []string

	// Files the previous agent owns (will be removed).
	if prev != nil {
		if rec, ok := m.Installed[prev.Key]; ok && len(rec.CreatedFiles) > 0 {
			diff = append(diff, "  remove "+joinShort(rec.CreatedFiles))
		}
	}

	// Files the new agent will create.
	if owned, ferr := agentOwnedFiles(target); ferr == nil && len(owned) > 0 {
		diff = append(diff, "  add "+joinShort(owned))
	}

	var msg strings.Builder
	msg.WriteString(prevName)
	msg.WriteString(" is currently installed.")
	if len(diff) > 0 {
		msg.WriteString("\nSwitching to ")
		msg.WriteString(target.Name)
		msg.WriteString(" would:\n")
		for _, line := range diff {
			msg.WriteString(line)
			msg.WriteByte('\n')
		}
		msg.WriteString("Re-run with --switch to apply.")
	} else {
		msg.WriteString(" Re-run with `--switch` to replace it with ")
		msg.WriteString(target.Name)
		msg.WriteByte('.')
	}
	_ = root
	_ = data
	return clierr.New(clierr.CodeAIAgentConflict, msg.String())
}

// joinShort joins paths with ", " for inline display. Truncates with
// "(+N more)" if the list is long enough that a single line gets
// unwieldy in a terminal.
func joinShort(paths []string) string {
	const maxInline = 4
	if len(paths) <= maxInline {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s (+%d more)", strings.Join(paths[:maxInline], ", "), len(paths)-maxInline)
}

// switchUninstall removes the currently-installed agent before the new
// one takes over. Used by the --switch path. In dryRun mode the
// uninstall is also a dry-run — no disk changes.
func switchUninstall(m *Manifest, root string, dryRun bool) error {
	prev := AgentByKey(m.ActiveAgent)
	if prev == nil {
		// Manifest references an agent the current CLI doesn't know
		// about (older or experimental). Just clear the active marker —
		// we have no template to walk for cleanup.
		m.ActiveAgent = ""
		return nil
	}
	rec, ok := m.Installed[prev.Key]
	if !ok {
		m.ActiveAgent = ""
		return nil
	}
	data, err := buildInstallData(root)
	if err != nil {
		return err
	}
	_, err = Uninstall(prev, root, rec, data, UninstallOptions{DryRun: dryRun})
	if err != nil {
		return err
	}
	if !dryRun {
		m.RecordUninstall(prev.Key)
	}
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
		fprintln(w, "  Open this project in Claude Code. It will read CLAUDE.md.")
		fprintln(w, "  Pre-approved commands: gofasta *, make *, go build/test/vet, gofmt, common read-only git")
		fprintln(w, "  Slash commands available: /verify, /scaffold, /inspect")
	case "cursor":
		fprintln(w, "  Open this project in Cursor. It reads AGENTS.md from the project root.")
	case "codex":
		fprintln(w, "  Run Codex from the project root. It reads AGENTS.md and .codex/config.toml.")
	case "aider":
		fprintln(w, "  Start `aider` from the project root. It will load CONVENTIONS.md and run `gofasta verify` after each edit.")
	case "windsurf":
		fprintln(w, "  Open this project in Windsurf. Cascade reads AGENTS.md.")
	}
}

// getwd is a package-level seam over os.Getwd so tests can simulate a
// process whose working directory has been deleted under it (a rare
// condition that would otherwise be uncoverable).
var getwd = os.Getwd

// findProjectRoot walks up from the current directory looking for a go.mod
// file. Returns the absolute path to the directory containing go.mod, or
// a clierr if not inside a Go module.
func findProjectRoot() (string, error) {
	cwd, err := getwd()
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
