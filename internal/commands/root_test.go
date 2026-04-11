package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestRootCmd_HasSubcommands(t *testing.T) {
	cmds := rootCmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}

	expectedCmds := []string{"new", "dev", "init", "migrate", "seed", "serve", "swagger", "generate", "wire", "upgrade", "version", "db", "doctor", "routes", "console", "deploy"}
	for _, expected := range expectedCmds {
		assert.Contains(t, names, expected, "rootCmd should have subcommand: %s", expected)
	}
}

func TestRootCmd_UseIsGofasta(t *testing.T) {
	assert.Equal(t, "gofasta", rootCmd.Use)
}

func TestRootCmd_HasShortDescription(t *testing.T) {
	assert.NotEmpty(t, rootCmd.Short)
}

func TestExecute_UnknownCommand(t *testing.T) {
	// rootCmd.Execute() calls os.Exit on error, which we can't test directly.
	// Instead, test that rootCmd returns an error for unknown subcommands.
	rootCmd.SetArgs([]string{"nonexistent-command"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	// Reset args
	rootCmd.SetArgs(nil)
}

func TestRootCmd_HasLongDescription(t *testing.T) {
	assert.NotEmpty(t, rootCmd.Long)
}

// --- Grouped help output ---

// runRootHelpWithGroups primes the root command with the auto-generated
// help+completion commands, assigns groups, captures the custom help
// output to a buffer, and returns it. Every grouped-help test uses this
// helper so cobra's lazy init runs in the right order.
func runRootHelpWithGroups(t *testing.T) string {
	t.Helper()
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignGroups()

	var buf bytes.Buffer
	origOut := rootCmd.OutOrStdout()
	rootCmd.SetOut(&buf)
	t.Cleanup(func() { rootCmd.SetOut(origOut) })

	printRootHelp(rootCmd)
	return buf.String()
}

func TestPrintRootHelp_IncludesEveryGroupHeader(t *testing.T) {
	out := runRootHelpWithGroups(t)
	for _, id := range groupOrder {
		assert.Contains(t, out, groupTitles[id]+":",
			"help output missing group header %q", groupTitles[id])
	}
}

func TestPrintRootHelp_ListsEveryTopLevelCommand(t *testing.T) {
	out := runRootHelpWithGroups(t)
	topLevel := []string{
		// Project lifecycle
		"new", "init", "doctor", "upgrade", "version",
		// Development workflow
		"dev", "serve", "console", "routes", "swagger", "wire",
		// Database
		"migrate", "seed", "db",
		// Code generation
		"generate",
		// Deployment
		"deploy",
		// Shell integration
		"completion", "help",
	}
	for _, name := range topLevel {
		assert.Contains(t, out, name, "help output missing command %q", name)
	}
}

func TestPrintRootHelp_ListsNestedSubcommands(t *testing.T) {
	out := runRootHelpWithGroups(t)
	nested := []string{
		"up", "down", // migrate
		"reset",                               // db
		"setup", "status", "logs", "rollback", // deploy
		"scaffold", "model", "controller", "dto", "job", "task", "email-template", // generate
		"bash", "zsh", "fish", "powershell", // completion
	}
	for _, name := range nested {
		assert.Contains(t, out, name, "help output missing nested subcommand %q", name)
	}
}

func TestPrintRootHelp_UsesCommandOutputWriter(t *testing.T) {
	// Sanity check that printRootHelp writes to cmd.OutOrStdout() rather
	// than bypassing it to os.Stdout — matters for tests that capture
	// output via SetOut().
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignGroups()
	printRootHelp(rootCmd)
	assert.Contains(t, buf.String(), "Usage:")
	assert.Contains(t, buf.String(), "[command] --help")
}

func TestAssignGroups_SetsKnownCommandGroups(t *testing.T) {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignGroups()
	want := map[string]string{
		"new":        groupLifecycle,
		"init":       groupLifecycle,
		"doctor":     groupLifecycle,
		"upgrade":    groupLifecycle,
		"version":    groupLifecycle,
		"dev":        groupWorkflow,
		"serve":      groupWorkflow,
		"console":    groupWorkflow,
		"routes":     groupWorkflow,
		"swagger":    groupWorkflow,
		"wire":       groupWorkflow,
		"migrate":    groupDatabase,
		"seed":       groupDatabase,
		"db":         groupDatabase,
		"generate":   groupGenerate,
		"deploy":     groupDeploy,
		"completion": groupShell,
		"help":       groupShell,
	}
	for _, c := range rootCmd.Commands() {
		if id, ok := want[c.Name()]; ok {
			assert.Equal(t, id, c.GroupID, "command %q has wrong group", c.Name())
		}
	}
}

func TestPrintRootHelp_SkipsEmptyGroupAndSortsMultipleUngrouped(t *testing.T) {
	// Exercise two branches of printRootHelp that the other tests don't reach:
	//   1. The empty-group `continue` — fires when a group in groupOrder has
	//      zero member commands. We temporarily clear the GroupID on every
	//      command in the Shell integration group so groupShell is empty.
	//   2. The ungrouped sort comparator — sort.SliceStable's `less` closure
	//      is only invoked when there are ≥2 items to compare. A single
	//      ungrouped command skips the closure entirely, so we add two.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignGroups()

	// Temporarily blank out groupShell membership.
	saved := map[string]string{}
	for _, c := range rootCmd.Commands() {
		if c.GroupID == groupShell {
			saved[c.Name()] = c.GroupID
			c.GroupID = ""
		}
	}
	t.Cleanup(func() {
		for name, id := range saved {
			for _, c := range rootCmd.Commands() {
				if c.Name() == name {
					c.GroupID = id
				}
			}
		}
	})

	// Two ungrouped commands out of alphabetical order so the sort has work
	// to do and the comparator closure actually runs.
	zulu := &cobra.Command{
		Use:   "zulu-ephemeral",
		Short: "throwaway — sorts last",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	alpha := &cobra.Command{
		Use:   "alpha-ephemeral",
		Short: "throwaway — sorts first",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	rootCmd.AddCommand(zulu, alpha)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(zulu)
		rootCmd.RemoveCommand(alpha)
	})

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	printRootHelp(rootCmd)

	out := buf.String()
	// Empty groupShell must not render its heading.
	assert.NotContains(t, out, "Shell integration:",
		"group with zero commands should be skipped entirely")
	// Both ungrouped commands must appear, and alpha before zulu.
	alphaIdx := strings.Index(out, "alpha-ephemeral")
	zuluIdx := strings.Index(out, "zulu-ephemeral")
	assert.NotEqual(t, -1, alphaIdx, "alpha-ephemeral missing from output")
	assert.NotEqual(t, -1, zuluIdx, "zulu-ephemeral missing from output")
	assert.Less(t, alphaIdx, zuluIdx,
		"ungrouped commands should be sorted alphabetically (alpha before zulu)")
}

func TestPrintRootHelp_RendersUngroupedAndSkipsHidden(t *testing.T) {
	// Attach two throwaway commands to rootCmd for the duration of this
	// test: one ungrouped (should render under "Additional commands") and
	// one hidden (should be filtered out entirely).
	ungrouped := &cobra.Command{
		Use:   "ephemeral-ungrouped",
		Short: "throwaway ungrouped test command",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	hidden := &cobra.Command{
		Use:    "ephemeral-hidden",
		Short:  "throwaway hidden test command",
		Hidden: true,
		Run:    func(_ *cobra.Command, _ []string) {},
	}
	rootCmd.AddCommand(ungrouped, hidden)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(ungrouped)
		rootCmd.RemoveCommand(hidden)
	})

	out := runRootHelpWithGroups(t)
	assert.Contains(t, out, "Additional commands:",
		"ungrouped command should trigger the Additional commands header")
	assert.Contains(t, out, "ephemeral-ungrouped")
	assert.NotContains(t, out, "ephemeral-hidden",
		"hidden commands should be filtered from the grouped listing")
}

func TestVisibleSubcommands_FiltersHelpAndHidden(t *testing.T) {
	// Make sure the filter drops the implicit "help" subcommand and any
	// command marked Hidden.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignGroups()
	subs := visibleSubcommands(rootCmd)
	for _, s := range subs {
		assert.NotEqual(t, "help", s.Name(), "help should not appear in visibleSubcommands")
		assert.False(t, s.Hidden, "hidden commands should be filtered")
	}
}
