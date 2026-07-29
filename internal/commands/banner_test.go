package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestPrintBanner(t *testing.T) {
	assert.NotPanics(t, printBanner)
}
