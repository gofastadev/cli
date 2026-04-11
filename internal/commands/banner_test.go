package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// Helper to swap the color-support detector and restore on cleanup.
// Also resets the bannerShown guard so each test starts fresh.
func withColorSupport(t *testing.T, truecolor, any bool) {
	t.Helper()
	orig := colorSupportFn
	colorSupportFn = func(_ io.Writer) (bool, bool) { return truecolor, any }
	resetBannerShown()
	t.Cleanup(func() {
		colorSupportFn = orig
		resetBannerShown()
	})
}

// Helper to swap the suppression decider.
func withBannerSuppressed(t *testing.T, suppressed bool) {
	t.Helper()
	orig := bannerSuppressedFn
	bannerSuppressedFn = func() bool { return suppressed }
	resetBannerShown()
	t.Cleanup(func() {
		bannerSuppressedFn = orig
		resetBannerShown()
	})
}

func TestWriteBanner_Truecolor(t *testing.T) {
	withColorSupport(t, true, true)
	withBannerSuppressed(t, false)

	var buf bytes.Buffer
	writeBanner(&buf)

	out := buf.String()
	assert.Contains(t, out, ansiCyanTrueColor, "should use 24-bit escape for truecolor terminals")
	assert.Contains(t, out, ansiReset, "should close color with reset")
	assert.Contains(t, out, "Gofasta", "tagline should name Gofasta")
	assert.Contains(t, out, tagline, "tagline should follow the art")
}

func TestWriteBanner_256Color(t *testing.T) {
	withColorSupport(t, false, true)
	withBannerSuppressed(t, false)

	var buf bytes.Buffer
	writeBanner(&buf)

	out := buf.String()
	assert.Contains(t, out, ansiCyan256, "should use 256-color fallback when truecolor unavailable")
	assert.NotContains(t, out, ansiCyanTrueColor, "must not emit 24-bit escape")
}

func TestWriteBanner_NoColor(t *testing.T) {
	withColorSupport(t, false, false)
	withBannerSuppressed(t, false)

	var buf bytes.Buffer
	writeBanner(&buf)

	out := buf.String()
	assert.NotContains(t, out, "\x1b[", "should emit no ANSI escapes when color is off")
	assert.Contains(t, out, "Gofasta", "tagline should still render in plain mode")
	assert.Contains(t, out, tagline)
	// The ASCII art uses slashes + underscores; verify something graphical is there.
	assert.Contains(t, out, "__ _")
}

func TestWriteBanner_Suppressed(t *testing.T) {
	withBannerSuppressed(t, true)

	var buf bytes.Buffer
	writeBanner(&buf)

	assert.Empty(t, buf.String(), "suppressed banner should write nothing")
}

func TestWriteBanner_NonFileWriter(t *testing.T) {
	// Default colorSupportFn (unmocked). A bytes.Buffer is not an *os.File,
	// so color must be disabled.
	t.Setenv("FORCE_COLOR", "") // make sure test env isn't overriding
	var buf bytes.Buffer
	writeBanner(&buf)
	assert.NotContains(t, buf.String(), "\x1b[", "non-TTY writer must receive plain output")
	assert.Contains(t, buf.String(), "Gofasta")
}

func TestColorSupportFn_RespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	tc, any := colorSupportFn(&bytes.Buffer{})
	assert.False(t, tc)
	assert.False(t, any)
}

func TestColorSupportFn_ForceColorOverridesNonTTY(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	tc, any := colorSupportFn(&bytes.Buffer{})
	assert.True(t, tc)
	assert.True(t, any)
}

func TestColorSupportFn_ForceColorZeroIsIgnored(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "0")
	tc, any := colorSupportFn(&bytes.Buffer{})
	assert.False(t, tc)
	assert.False(t, any)
}

func TestBannerSuppressedFn_GofastaNoBanner(t *testing.T) {
	t.Setenv("GOFASTA_NO_BANNER", "1")
	assert.True(t, bannerSuppressedFn())
}

func TestBannerSuppressedFn_GofastaNoBannerZero(t *testing.T) {
	t.Setenv("GOFASTA_NO_BANNER", "0")
	assert.False(t, bannerSuppressedFn())
}

func TestBannerSuppressedFn_Unset(t *testing.T) {
	t.Setenv("GOFASTA_NO_BANNER", "")
	assert.False(t, bannerSuppressedFn())
}

// --- shouldSkipBanner ---

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

// --- PersistentPreRun integration: the banner should be invoked on
// any subcommand that has a parent rooted at rootCmd. We don't run the
// real subcommands (they have side effects) — just verify the hook fires.

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

// --- isTTYFn ---

// On a *os.File that's a real pipe (not a TTY), isTTYFn should return
// (false, nil) — no error, just not a terminal. This exercises the
// Mode()&ModeCharDevice == 0 branch.
func TestIsTTYFn_PipeIsNotTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })

	tty, err := isTTYFn(w)
	assert.NoError(t, err)
	assert.False(t, tty, "a pipe writer is never a terminal")
}

// Non-*os.File writers (e.g. a buffer) return (false, nil).
func TestIsTTYFn_NonFileWriter(t *testing.T) {
	tty, err := isTTYFn(&bytes.Buffer{})
	assert.NoError(t, err)
	assert.False(t, tty)
}

// Closed *os.File should cause Stat() to return an error, which isTTYFn
// must propagate. This covers the `if err != nil` branch.
func TestIsTTYFn_StatError(t *testing.T) {
	f, err := os.CreateTemp("", "gofasta-isttyfn-*")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	path := f.Name()
	t.Cleanup(func() { _ = os.Remove(path) })
	// Close the file first, then construct a new *os.File around its now-invalid
	// file descriptor so Stat() returns an error.
	_ = f.Close()
	bad := os.NewFile(f.Fd(), path)
	defer func() { _ = bad.Close() }()

	_, err = isTTYFn(bad)
	assert.Error(t, err, "Stat() on a closed file descriptor should error")
}

// --- colorSupportFn: real code path (unmocked) ---

// When isTTYFn is mocked to return (true, nil) and COLORTERM is set to
// "truecolor", colorSupportFn should hit the truecolor branch and return
// (true, true). This exercises lines 80-82 of banner.go.
func TestColorSupportFn_TruecolorViaColorterm(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("COLORTERM", "truecolor")

	origTTY := isTTYFn
	isTTYFn = func(io.Writer) (bool, error) { return true, nil }
	t.Cleanup(func() { isTTYFn = origTTY })

	tc, any := colorSupportFn(&bytes.Buffer{})
	assert.True(t, tc, "COLORTERM=truecolor should enable 24-bit mode")
	assert.True(t, any)
}

// Same as above but with COLORTERM=24bit (alternate spelling).
func TestColorSupportFn_Truecolor24bit(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("COLORTERM", "24bit")

	origTTY := isTTYFn
	isTTYFn = func(io.Writer) (bool, error) { return true, nil }
	t.Cleanup(func() { isTTYFn = origTTY })

	tc, any := colorSupportFn(&bytes.Buffer{})
	assert.True(t, tc)
	assert.True(t, any)
}

// TTY with no COLORTERM → falls back to 256-color (true, false → false, true).
func TestColorSupportFn_TTYWithout24bit(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("COLORTERM", "")

	origTTY := isTTYFn
	isTTYFn = func(io.Writer) (bool, error) { return true, nil }
	t.Cleanup(func() { isTTYFn = origTTY })

	tc, any := colorSupportFn(&bytes.Buffer{})
	assert.False(t, tc, "COLORTERM unset should NOT enable 24-bit")
	assert.True(t, any, "TTY without COLORTERM still supports 256-color")
}

// isTTYFn returning an error → colorSupportFn must bail out with no color.
func TestColorSupportFn_TTYError(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")

	origTTY := isTTYFn
	isTTYFn = func(io.Writer) (bool, error) { return false, fmt.Errorf("stat bad") }
	t.Cleanup(func() { isTTYFn = origTTY })

	tc, any := colorSupportFn(&bytes.Buffer{})
	assert.False(t, tc)
	assert.False(t, any)
}

// --- shouldSkipBanner: deep command tree walk ---

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

// --- PersistentPreRun: the skip branch ---

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

// --- Execute (wrapper around runExecute + osExit) ---

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

// --- rootCmd.Run (bare `gofasta`) ---

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

// --- rootCmd.HelpFunc (wraps default) ---

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
