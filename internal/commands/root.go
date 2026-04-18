package commands

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/generate"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// Command group IDs — mirror the structure of the whitepaper §4.2 tables so
// the grouped listing users see matches the documentation one-to-one.
const (
	groupLifecycle = "lifecycle"
	groupWorkflow  = "workflow"
	groupDatabase  = "database"
	groupGenerate  = "generate"
	groupDeploy    = "deploy"
	groupShell     = "shell"
)

// groupTitles maps a group ID to its human-readable heading. Keep in sync
// with the section headings in the whitepaper.
var groupTitles = map[string]string{
	groupLifecycle: "Project lifecycle",
	groupWorkflow:  "Development workflow",
	groupDatabase:  "Database",
	groupGenerate:  "Code generation",
	groupDeploy:    "Deployment",
	groupShell:     "Shell integration",
}

// groupOrder is the display order — alphabetical is wrong, we want lifecycle
// first so a new user reads the list in the order they'd actually use it.
var groupOrder = []string{
	groupLifecycle,
	groupWorkflow,
	groupDatabase,
	groupGenerate,
	groupDeploy,
	groupShell,
}

// noBanner is set by the global --no-banner flag. When true, the banner
// is suppressed even if the environment would otherwise show it.
var noBanner bool

// jsonOutput is set by the global --json flag. When true, every
// structured-output subcommand emits machine-parseable JSON instead of
// human-formatted text, and the banner is suppressed unconditionally.
// The value is mirrored into the cliout package via SetJSONMode at
// start-up so every subcommand reads it through a single source of truth.
var jsonOutput bool

// commandsWithoutBanner lists subcommands whose output must stay
// machine-parseable. Showing a decorative banner on top of these would
// break shell-completion scripts, scrape-friendly `--version` output, or
// wrap long-lived streaming commands in noise.
var commandsWithoutBanner = map[string]bool{
	"completion": true,
	"__complete": true, // cobra internal completion helper
}

// shouldSkipBanner returns true when the invocation should not emit the
// banner: explicit opt-out, machine-output commands, bare --version, or
// --json mode (which must emit strictly parseable output).
func shouldSkipBanner(cmd *cobra.Command) bool {
	if noBanner {
		return true
	}
	if jsonOutput {
		return true
	}
	// `gofasta --version` / `gofasta -v` is widely parsed by shell scripts
	// and package managers. Keep its output clean.
	if versionFlag, err := cmd.Flags().GetBool("version"); err == nil && versionFlag {
		return true
	}
	// Walk up the command tree to find the top-level command name, then
	// look it up in the deny list.
	root := cmd
	for root.HasParent() && root.Parent().HasParent() {
		root = root.Parent()
	}
	return commandsWithoutBanner[root.Name()]
}

var rootCmd = &cobra.Command{
	Use:   "gofasta",
	Short: "Gofasta — scaffold and manage Go backend projects built with the Gofasta toolkit",
	Long: `Gofasta is a CLI for scaffolding Go backend projects and generating
idiomatic code on top of the Gofasta library. It creates new projects from
a template, generates resources (models, services, controllers, migrations,
DTOs, Wire providers), runs dev servers and migrations, deploys to remote
hosts, and self-updates.

The CLI is a standalone binary that does not import the Gofasta library —
it only manipulates files on disk.`,
	SilenceUsage: true,
	// PersistentPreRun fires before every Run / RunE, for the root command
	// AND every subcommand in the tree — the exact hook we want for
	// mirroring the --json flag into cliout and printing the banner.
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		// Mirror the --json flag into cliout before anything else runs,
		// so every subcommand reads the same value via cliout.JSON().
		cliout.SetJSONMode(jsonOutput)
		if shouldSkipBanner(cmd) {
			return
		}
		printBanner()
	},
	// Bare `gofasta` (no subcommand, no args) prints banner + help.
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&noBanner, "no-banner", false,
		"Suppress the branded banner shown before each command (also honored via GOFASTA_NO_BANNER=1)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false,
		"Emit machine-parseable JSON output (and suppress the banner). Intended for AI agents and CI automation.")

	// Register command groups so every top-level command can be placed into a
	// section that mirrors the whitepaper. Groups are matched by ID when a
	// subcommand sets its GroupID.
	for _, id := range groupOrder {
		rootCmd.AddGroup(&cobra.Group{ID: id, Title: groupTitles[id] + ":"})
	}

	// `--help` output bypasses PersistentPreRun — cobra routes help through a
	// dedicated HelpFunc, not the Run chain. We replace the root HelpFunc
	// entirely (not wrap) so we can emit a grouped + nested command listing
	// that matches the whitepaper §4.2 structure. For subcommands we still
	// fall back to cobra's default help.
	defaultHelpFn := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if !shouldSkipBanner(cmd) {
			printBanner()
		}
		if cmd == rootCmd {
			printRootHelp(cmd)
			return
		}
		defaultHelpFn(cmd, args)
	})

	rootCmd.AddCommand(generate.Cmd)
	rootCmd.AddCommand(generate.WireCmd)

	// Group assignment is deferred to runExecute because Go's init order
	// is per-file — root.go's init can run before the init of files like
	// upgrade.go / version.go that register their commands to rootCmd, so
	// calling assignGroups() here would miss them. Doing it at Execute
	// time guarantees every command has been attached.
}

// commandGroupAssignments maps top-level command Use to its group ID.
// Kept in one place so adding a new command is a one-line change.
var commandGroupAssignments = map[string]string{
	// Project lifecycle
	"new":     groupLifecycle,
	"init":    groupLifecycle,
	"doctor":  groupLifecycle,
	"upgrade": groupLifecycle,
	"version": groupLifecycle,
	// Development workflow
	"dev":     groupWorkflow,
	"serve":   groupWorkflow,
	"console": groupWorkflow,
	"routes":  groupWorkflow,
	"swagger": groupWorkflow,
	"wire":    groupWorkflow,
	"verify":  groupWorkflow,
	"status":  groupWorkflow,
	// Database
	"migrate": groupDatabase,
	"seed":    groupDatabase,
	"db":      groupDatabase,
	// Code generation
	"generate": groupGenerate,
	// Deployment
	"deploy": groupDeploy,
	// Shell integration
	"completion": groupShell,
	"help":       groupShell,
}

// assignGroups walks every registered top-level command and sets its GroupID
// from commandGroupAssignments. Called from init once every subcommand file
// has run its own init and attached its command to rootCmd.
func assignGroups() {
	for _, c := range rootCmd.Commands() {
		// Cobra's `Use` can carry positional hints like "new [project-name]"
		// — extract just the verb.
		name := strings.Fields(c.Use)[0]
		if id, ok := commandGroupAssignments[name]; ok {
			c.GroupID = id
		}
	}
}

// fprintln / fprintf / fprint are internal wrappers that swallow the write
// errors returned by fmt.Fprint*. Progress output to stdout is fire-and-
// forget — if the writer has gone away there is nothing actionable to do,
// and errcheck flags every unhandled return value. Using these wrappers
// keeps the help-rendering code readable instead of prefixing every line
// with `_, _ =`.
func fprintln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}

func fprintf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func fprint(w io.Writer, a ...any) {
	_, _ = fmt.Fprint(w, a...)
}

// printRootHelp renders a grouped, nested command listing for the root
// command. The output mirrors whitepaper §4.2 so users see the same
// hierarchy in the terminal and in the docs. Subcommands are indented
// under their parent and every command shows its short description.
// Writes go to cmd.OutOrStdout() so tests and callers that redirect
// output via cobra's SetOut() see the content.
func printRootHelp(cmd *cobra.Command) {
	w := cmd.OutOrStdout()

	// Header — long description, not short, so the root help reads like
	// the opening paragraph of the whitepaper.
	fprintln(w, cmd.Long)
	fprintln(w)

	fprintln(w, termcolor.CBold("Usage:"))
	fprintf(w, "  %s [flags]\n", cmd.CommandPath())
	fprintf(w, "  %s [command]\n", cmd.CommandPath())
	fprintln(w)

	// Group top-level commands by GroupID.
	byGroup := map[string][]*cobra.Command{}
	var ungrouped []*cobra.Command
	for _, c := range cmd.Commands() {
		if c.Hidden || !c.IsAvailableCommand() && c.Name() != "help" {
			continue
		}
		if c.GroupID == "" {
			ungrouped = append(ungrouped, c)
			continue
		}
		byGroup[c.GroupID] = append(byGroup[c.GroupID], c)
	}

	// Render each group in whitepaper order.
	for _, id := range groupOrder {
		cmds := byGroup[id]
		if len(cmds) == 0 {
			continue
		}
		sort.SliceStable(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
		fprintln(w, termcolor.CBold(termcolor.CBrand(groupTitles[id]+":")))
		printCommandList(w, cmds, 1)
		fprintln(w)
	}

	if len(ungrouped) > 0 {
		sort.SliceStable(ungrouped, func(i, j int) bool { return ungrouped[i].Name() < ungrouped[j].Name() })
		fprintln(w, termcolor.CBold("Additional commands:"))
		printCommandList(w, ungrouped, 1)
		fprintln(w)
	}

	// Flags block — use cobra's existing rendering so formatting stays
	// consistent with subcommand help.
	if flagsUsage := cmd.LocalFlags().FlagUsages(); flagsUsage != "" {
		fprintln(w, termcolor.CBold("Flags:"))
		fprint(w, flagsUsage)
		fprintln(w)
	}

	fprintf(w, "Use \"%s\" for more information about a command.\n",
		termcolor.CBold(cmd.CommandPath()+" [command] --help"))
}

// printCommandList writes a tree of commands as a tab-aligned two-column
// table (name, short description) to w. Subcommands are printed immediately
// below their parent, indented by two spaces per nesting level. A single
// tabwriter is used for the whole tree so column alignment stays consistent
// across every depth — tabwriter needs all rows in one pass to compute
// column widths.
func printCommandList(w io.Writer, cmds []*cobra.Command, depth int) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	walkAndWrite(tw, cmds, depth)
	_ = tw.Flush()
}

// walkAndWrite is the recursive half of printCommandList. It writes each
// command's row and then immediately walks that command's children so the
// children appear under the parent instead of after all siblings.
func walkAndWrite(tw *tabwriter.Writer, cmds []*cobra.Command, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, c := range cmds {
		fprintf(tw, "%s%s\t%s\n", indent, termcolor.CBold(c.Name()), termcolor.CDim(c.Short))
		children := visibleSubcommands(c)
		if len(children) > 0 {
			sort.SliceStable(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
			walkAndWrite(tw, children, depth+1)
		}
	}
}

// visibleSubcommands returns the subcommands of c that should appear in
// the grouped listing — hidden ones and cobra's implicit "help" are
// filtered out so the tree stays tight.
func visibleSubcommands(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, sc := range c.Commands() {
		if sc.Hidden || sc.Name() == "help" {
			continue
		}
		out = append(out, sc)
	}
	return out
}

// runExecute is the testable core: runs the root command with the given
// version and returns any error instead of calling os.Exit.
func runExecute(version string) error {
	rootCmd.Version = version
	// Cobra lazily attaches `help` and `completion` commands inside Execute.
	// We force them to attach now so assignGroups can place them in the
	// Shell integration group — otherwise they'd fall through to the
	// ungrouped "Additional commands" section.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignGroups()
	return rootCmd.Execute()
}

// osExit is a package-level seam so tests can exercise Execute's error
// path without actually terminating the test binary.
var osExit = os.Exit

// Execute runs the root command with the given version string. Errors
// returned by the command tree are rendered through cliout.PrintError
// so --json mode serializes them via MarshalJSON (clierr.Error supports
// this) and text mode falls back to err.Error().
func Execute(version string) {
	if err := runExecute(version); err != nil {
		cliout.PrintError(err)
		osExit(1)
	}
}
