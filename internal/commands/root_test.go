package commands

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipBanner_NoBannerFlag(t *testing.T) {
	origNoBanner := noBanner
	noBanner = true
	t.Cleanup(func() { noBanner = origNoBanner })

	c := &cobra.Command{Use: "dev"}
	assert.True(t, shouldSkipBanner(c))
}

func TestShouldSkipBanner_VersionFlag(t *testing.T) {
	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	c := &cobra.Command{Use: "gofasta"}
	c.Flags().Bool("version", true, "")
	_ = c.Flags().Set("version", "true")
	assert.True(t, shouldSkipBanner(c))
}

func TestShouldSkipBanner_CompletionSubcommand(t *testing.T) {
	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	root := &cobra.Command{Use: "gofasta"}
	completion := &cobra.Command{Use: "completion"}
	root.AddCommand(completion)

	assert.True(t, shouldSkipBanner(completion))
}

func TestShouldSkipBanner_NormalCommand(t *testing.T) {
	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	root := &cobra.Command{Use: "gofasta"}
	dev := &cobra.Command{Use: "dev"}
	root.AddCommand(dev)

	assert.False(t, shouldSkipBanner(dev))
}

func TestRootCmd_PersistentPreRun_InvokesBanner(t *testing.T) {
	// Swap bannerStream to a buffer we can read back.
	origStream := bannerStream
	var buf bytes.Buffer
	bannerStream = &buf
	t.Cleanup(func() { bannerStream = origStream })

	// Disable suppression; force "any" color (256) so output is deterministic.
	withBannerSuppressed(t, false)
	withColorSupport(t, false, true)

	// Reset --no-banner in case a prior test left it toggled.
	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	// Invoke the PersistentPreRun directly on the `version` subcommand.
	// (We don't use rootCmd.Execute because that would also Run the command.)
	root := rootCmd
	ver, _, err := root.Find([]string{"version"})
	if err != nil {
		t.Fatalf("version subcommand not registered: %v", err)
	}
	root.PersistentPreRun(ver, nil)

	out := buf.String()
	assert.Contains(t, out, "Gofasta")
	assert.Contains(t, out, ansiCyan256)
}

func TestRootCmd_PersistentPreRun_SkipsCompletion(t *testing.T) {
	origStream := bannerStream
	var buf bytes.Buffer
	bannerStream = &buf
	t.Cleanup(func() { bannerStream = origStream })

	withBannerSuppressed(t, false)
	withColorSupport(t, false, true)

	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	root := rootCmd
	// Cobra auto-adds a `completion` subcommand; walk the tree to find it.
	var completion *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			completion = c
			break
		}
	}
	if completion == nil {
		t.Skip("cobra did not auto-register a completion subcommand in this version")
	}

	root.PersistentPreRun(completion, nil)
	assert.Empty(t, strings.TrimSpace(buf.String()),
		"banner must be suppressed for completion output")
}

// When a command is nested three levels deep (root → group → leaf), the
// for-loop in shouldSkipBanner must walk up past the group to find the
// top-level parent name. This exercises the loop body (lines 38-40 of root.go).
func TestShouldSkipBanner_ThreeLevelTreeUnderCompletion(t *testing.T) {
	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	root := &cobra.Command{Use: "gofasta"}
	completion := &cobra.Command{Use: "completion"}
	bash := &cobra.Command{Use: "bash"}
	completion.AddCommand(bash)
	root.AddCommand(completion)

	// bash is two levels deep under root; the walk must climb from bash → completion → root,
	// find `completion` as the top-level name, and skip.
	assert.True(t, shouldSkipBanner(bash),
		"a leaf under `completion` should be recognized via tree walk")
}

// Same tree shape but under a normal command — must NOT skip.
func TestShouldSkipBanner_ThreeLevelTreeUnderNormalCommand(t *testing.T) {
	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	root := &cobra.Command{Use: "gofasta"}
	deploy := &cobra.Command{Use: "deploy"}
	logs := &cobra.Command{Use: "logs"}
	deploy.AddCommand(logs)
	root.AddCommand(deploy)

	assert.False(t, shouldSkipBanner(logs),
		"a leaf under a normal group should not trigger skip")
}

// When shouldSkipBanner returns true, PersistentPreRun must return without
// invoking printBanner. Verify by toggling noBanner=true and checking
// that the output stream stays empty.
func TestRootCmd_PersistentPreRun_SkipBranch(t *testing.T) {
	origStream := bannerStream
	var buf bytes.Buffer
	bannerStream = &buf
	t.Cleanup(func() { bannerStream = origStream })

	withBannerSuppressed(t, false)
	withColorSupport(t, false, true)

	origNoBanner := noBanner
	noBanner = true // force shouldSkipBanner → true
	t.Cleanup(func() { noBanner = origNoBanner })

	root := rootCmd
	ver, _, err := root.Find([]string{"version"})
	if err != nil {
		t.Fatalf("version subcommand not registered: %v", err)
	}
	root.PersistentPreRun(ver, nil)

	assert.Empty(t, buf.String(),
		"PersistentPreRun must not write anything when shouldSkipBanner is true")
}

// Happy path: runExecute returns nil → Execute returns without touching osExit.
func TestExecute_Success(t *testing.T) {
	// Capture any attempted exit so a stray osExit call fails the test loudly.
	var exitCalled bool
	origExit := osExit
	osExit = func(int) { exitCalled = true }
	t.Cleanup(func() { osExit = origExit })

	// Ask for the built-in version flag so rootCmd.Execute() succeeds quickly
	// without running any real subcommand logic.
	rootCmd.SetArgs([]string{"--version"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	Execute("0.0.0-test")

	assert.False(t, exitCalled, "success path must not call osExit")
}

// Error path: bogus subcommand → runExecute returns an error → Execute must
// call osExit(1). The seam lets us observe the call without actually exiting.
func TestExecute_ErrorCallsOsExit(t *testing.T) {
	var exitCode int
	var exitCalled bool
	origExit := osExit
	osExit = func(code int) {
		exitCalled = true
		exitCode = code
	}
	t.Cleanup(func() { osExit = origExit })

	rootCmd.SetArgs([]string{"definitely-not-a-real-subcommand"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	Execute("0.0.0-test")

	assert.True(t, exitCalled, "error path must call osExit")
	assert.Equal(t, 1, exitCode, "non-zero exit code on error")
}

// Bare `gofasta` with no subcommand and no args should hit the Run closure,
// which calls cmd.Help(). Route output through a buffer to avoid touching
// the real stderr/stdout.
func TestRootCmd_RunInvokesHelp(t *testing.T) {
	// Redirect banner to a buffer so we can observe it, and mute cobra's
	// own help output by redirecting the command's output streams.
	origStream := bannerStream
	var bannerBuf bytes.Buffer
	bannerStream = &bannerBuf
	t.Cleanup(func() { bannerStream = origStream })

	var helpBuf bytes.Buffer
	rootCmd.SetOut(&helpBuf)
	rootCmd.SetErr(&helpBuf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	withBannerSuppressed(t, false)
	withColorSupport(t, false, true)

	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	// Invoke Run directly on rootCmd — this hits the closure at root.go:66-68.
	rootCmd.Run(rootCmd, nil)

	// cmd.Help() calls the registered HelpFunc, which prints the banner first
	// and then falls through to cobra's default help renderer that writes to
	// the command's output writer.
	assert.Contains(t, helpBuf.String(), "Usage:",
		"cmd.Help() should render cobra's standard usage block")
}

// Call the HelpFunc closure directly to cover the `if !shouldSkipBanner`
// branch and the `printBanner()` + `defaultHelpFn` calls inside it.
func TestRootCmd_HelpFunc_ShowsBanner(t *testing.T) {
	origStream := bannerStream
	var bannerBuf bytes.Buffer
	bannerStream = &bannerBuf
	t.Cleanup(func() { bannerStream = origStream })

	var helpBuf bytes.Buffer
	rootCmd.SetOut(&helpBuf)
	rootCmd.SetErr(&helpBuf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	withBannerSuppressed(t, false)
	withColorSupport(t, false, true)

	origNoBanner := noBanner
	noBanner = false
	t.Cleanup(func() { noBanner = origNoBanner })

	// Invoke HelpFunc on the `version` subcommand — covers the closure body
	// including the shouldSkipBanner check, printBanner, and defaultHelpFn.
	ver, _, err := rootCmd.Find([]string{"version"})
	if err != nil {
		t.Fatalf("version subcommand not registered: %v", err)
	}
	rootCmd.HelpFunc()(ver, nil)

	assert.Contains(t, bannerBuf.String(), "Gofasta",
		"HelpFunc closure should invoke printBanner, which writes to bannerStream")
	assert.Contains(t, helpBuf.String(), "Usage:",
		"HelpFunc closure should delegate to defaultHelpFn, which writes Usage: to the command output")
}

// When shouldSkipBanner is true, the HelpFunc closure must still call
// defaultHelpFn but NOT invoke printBanner.
func TestRootCmd_HelpFunc_SkipsBannerWhenNoBanner(t *testing.T) {
	origStream := bannerStream
	var bannerBuf bytes.Buffer
	bannerStream = &bannerBuf
	t.Cleanup(func() { bannerStream = origStream })

	var helpBuf bytes.Buffer
	rootCmd.SetOut(&helpBuf)
	rootCmd.SetErr(&helpBuf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	withBannerSuppressed(t, false)
	withColorSupport(t, false, true)

	origNoBanner := noBanner
	noBanner = true // force skip
	t.Cleanup(func() { noBanner = origNoBanner })

	ver, _, err := rootCmd.Find([]string{"version"})
	if err != nil {
		t.Fatalf("version subcommand not registered: %v", err)
	}
	rootCmd.HelpFunc()(ver, nil)

	assert.Empty(t, bannerBuf.String(),
		"skip branch must not write to bannerStream")
	assert.Contains(t, helpBuf.String(), "Usage:",
		"help output must still be rendered even when banner is skipped")
}

// rootCmd.Run (the Run func that shows help + banner) is invoked when no subcommand is given.
func TestRootCmd_Run_Help(t *testing.T) {
	// rootCmd.SetArgs([]string{}) with no Execute call still goes through runExecute("")
	rootCmd.SetArgs([]string{})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	assert.NoError(t, runExecute("test"))
}

func TestRunExecute_Help(t *testing.T) {
	// With no subcommand, cobra runs the root Run func which calls printBanner + Help
	rootCmd.SetArgs([]string{"version"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	assert.NoError(t, runExecute("0.0.0-test"))
}

func TestRunExecute_UnknownSubcommand(t *testing.T) {
	rootCmd.SetArgs([]string{"definitely-not-a-cmd"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	assert.Error(t, runExecute("0.0.0-test"))
}

// TestHandleIndex_ExecuteError — the template loads but Execute
// fails at runtime.
func TestHandleIndex_ExecuteError(t *testing.T) {
	orig := loadDashboardTemplateFn
	// Build a real parseable template whose Execute errors at runtime.
	tmpl, err := template.New("t").Parse(`{{call .NoSuchFunc}}`)
	require.NoError(t, err)
	loadDashboardTemplateFn = func() (*template.Template, error) { return tmpl, nil }
	t.Cleanup(func() { loadDashboardTemplateFn = orig })
	srv := &dashboardServer{}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

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

// TestShouldSkipBanner_JSON — jsonOutput=true returns true.
func TestShouldSkipBanner_JSON(t *testing.T) {
	orig := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = orig })
	assert.True(t, shouldSkipBanner(rootCmd))
}

var errDummy = errors.New("dummy")

// chdirTemp creates a new temp dir and cd's into it for the duration of the test.
func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
}

// stripANSI removes any ESC-[…m escape sequence so tests don't have
// to hardcode the color codes termcolor emits on TTY output.
func stripANSI(s string) string {
	var out bytes.Buffer
	skip := false
	for _, r := range s {
		switch {
		case skip:
			if r == 'm' {
				skip = false
			}
		case r == '\x1b':
			skip = true
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// captureStdout redirects os.Stdout into an in-memory buffer while fn
// runs, then restores it. Used by JSON-mode tests so we can assert the
// emitted NDJSON without polluting the test runner's own output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w

	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()
	wg.Wait()
	return buf.String()
}

// errStub is a sentinel test error.
var errStub = stubErr("stub error")

type stubErr string

func (s stubErr) Error() string { return string(s) }

// withJSONMode flips cliout into JSON mode for the test and restores
// it on cleanup. Centralizes the toggle so individual tests don't have
// to remember to defer the restore.
func withJSONMode(t *testing.T) {
	t.Helper()
	cliout.SetJSONMode(true)
	t.Cleanup(func() { cliout.SetJSONMode(false) })
}

// helper: switch cwd into a temp dir for the duration of one test, ensure
// the original cwd is restored regardless of the test outcome.
func chdirTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func codeOf(err error) string {
	var ce *clierr.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}
