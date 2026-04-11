package commands

import (
	"bytes"
	"io"
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
