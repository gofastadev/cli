package termcolor

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	assert.Equal(t, ModeNone, Detect())
	assert.False(t, Enabled())
}

func TestDetect_ForceColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	assert.Equal(t, ModeTrueColor, Detect())
	assert.True(t, Enabled())
}

func TestDetect_ForceColorZeroIsOff(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "0")
	// FORCE_COLOR=0 should fall through to the TTY/COLORTERM check. With a
	// bytes.Buffer as Out, isTTY returns false so Detect returns ModeNone.
	prev := Out
	Out = &bytes.Buffer{}
	t.Cleanup(func() { Out = prev })
	assert.Equal(t, ModeNone, Detect())
}

func TestDetect_NonTTY(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	prev := Out
	Out = &bytes.Buffer{}
	t.Cleanup(func() { Out = prev })
	assert.Equal(t, ModeNone, Detect())
}

func TestSetModeForTest(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	assert.Equal(t, ModeTrueColor, Detect())
	restore()
	// After restore, Detect falls back to environment — in tests that's
	// typically ModeNone because stdout is piped.
	assert.NotEqual(t, ModeTrueColor, Detect())
}

func TestC_Disabled(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()
	assert.Equal(t, "hello", C(Red, "hello"))
}

func TestC_Enabled(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := C(Red, "hello")
	assert.Contains(t, got, Red)
	assert.Contains(t, got, "hello")
	assert.Contains(t, got, Reset)
}

func TestSemanticWrappers(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	assert.Contains(t, CBold("x"), Bold)
	assert.Contains(t, CDim("x"), Dim)
	assert.Contains(t, CGreen("x"), Green)
	assert.Contains(t, CYellow("x"), Yellow)
	assert.Contains(t, CRed("x"), Red)
	assert.Contains(t, CBlue("x"), Blue)
}

// TestFail — cover the Fail string-returning helper directly so the
// ✗ + formatted-args composition is verified.
func TestFail(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := Fail("oops %s", "now")
	assert.Contains(t, got, "✗ ")
	assert.Contains(t, got, "oops now")
	assert.Contains(t, got, Red)
}

func TestCBrand_TrueColor(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	assert.Contains(t, CBrand("gofasta"), BrandTrueColor)
}

func TestCBrand_256(t *testing.T) {
	restore := SetModeForTest(Mode256)
	defer restore()
	got := CBrand("gofasta")
	assert.Contains(t, got, Brand256)
	assert.NotContains(t, got, BrandTrueColor)
}

func TestCBrand_Disabled(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()
	assert.Equal(t, "gofasta", CBrand("gofasta"))
}

func TestIsTTY_NonFile(t *testing.T) {
	// bytes.Buffer is not an *os.File — should return false.
	assert.False(t, isTTY(&bytes.Buffer{}))
}

// withDevNullOut points Out at /dev/null which is a character device, so
// isTTY returns true. This lets us exercise Detect's post-TTY branches in
// a test environment (where os.Stdout is a pipe and therefore not a TTY).
func withDevNullOut(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("/dev/null is unix-specific")
	}
	f, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	require.NoError(t, err)
	prev := Out
	Out = f
	t.Cleanup(func() {
		Out = prev
		_ = f.Close()
	})
}

func TestIsTTY_DevNull(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/null is unix-specific")
	}
	f, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	// /dev/null has os.ModeCharDevice set, so it counts as a TTY here.
	assert.True(t, isTTY(f))
}

func TestIsTTY_StatError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("closed-file Stat behavior varies on Windows")
	}
	f, err := os.CreateTemp(t.TempDir(), "stat-err-*")
	require.NoError(t, err)
	// Close then call Stat on the underlying *os.File — on unix this
	// returns "bad file descriptor" and isTTY should treat it as non-TTY.
	_ = f.Close()
	assert.False(t, isTTY(f))
}

func TestDetect_TTY_TrueColorViaCOLORTERM(t *testing.T) {
	withDevNullOut(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("COLORTERM", "truecolor")
	assert.Equal(t, ModeTrueColor, Detect())
}

func TestDetect_TTY_24bit(t *testing.T) {
	withDevNullOut(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("COLORTERM", "24bit")
	assert.Equal(t, ModeTrueColor, Detect())
}

func TestDetect_TTY_CaseInsensitiveCOLORTERM(t *testing.T) {
	withDevNullOut(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("COLORTERM", "TrueColor")
	assert.Equal(t, ModeTrueColor, Detect())
}

func TestDetect_TTY_Fallback256(t *testing.T) {
	withDevNullOut(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	// COLORTERM unset or unknown → 256-color fallback.
	t.Setenv("COLORTERM", "")
	assert.Equal(t, Mode256, Detect())
}

func TestDetect_TTY_UnknownCOLORTERM(t *testing.T) {
	withDevNullOut(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("COLORTERM", "xterm-16color")
	assert.Equal(t, Mode256, Detect())
}

func TestDetect_NoColorBeatsForceColor(t *testing.T) {
	withDevNullOut(t)
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")
	// NO_COLOR takes precedence over FORCE_COLOR per the no-color.org spec.
	assert.Equal(t, ModeNone, Detect())
}

// --- Exhaustive constant assertions ---
//
// These guard against accidental edits to the escape constants — the exact
// bytes matter because users' terminals parse them. If someone changes a
// constant, these tests force them to acknowledge it.

func TestEscapeConstants(t *testing.T) {
	assert.Equal(t, "\x1b[0m", Reset)
	assert.Equal(t, "\x1b[1m", Bold)
	assert.Equal(t, "\x1b[2m", Dim)
	assert.Equal(t, "\x1b[32m", Green)
	assert.Equal(t, "\x1b[33m", Yellow)
	assert.Equal(t, "\x1b[31m", Red)
	assert.Equal(t, "\x1b[34m", Blue)
	assert.Equal(t, "\x1b[38;2;0;173;216m", BrandTrueColor)
	assert.Equal(t, "\x1b[38;5;38m", Brand256)
}

// --- Semantic wrapper exhaustive tests (disabled mode) ---

func TestSemanticWrappers_Disabled(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()
	// Every wrapper must return the plain string unchanged when color is off.
	for name, got := range map[string]string{
		"CBold":   CBold("x"),
		"CDim":    CDim("x"),
		"CGreen":  CGreen("x"),
		"CYellow": CYellow("x"),
		"CRed":    CRed("x"),
		"CBlue":   CBlue("x"),
		"CBrand":  CBrand("x"),
	} {
		assert.Equal(t, "x", got, "%s should pass through when color disabled", name)
	}
}

// --- Empty-string handling ---

func TestC_EmptyString(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	// C("", "") is a weird but legal call — should still produce escapes.
	got := C(Red, "")
	assert.Equal(t, Red+Reset, got)
}

func TestCBrand_EmptyString(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	assert.Equal(t, BrandTrueColor+Reset, CBrand(""))
}

// --- Enabled() agreement with Detect() ---

func TestEnabled_AgreesWithDetect(t *testing.T) {
	for _, m := range []Mode{ModeNone, Mode256, ModeTrueColor} {
		restore := SetModeForTest(m)
		assert.Equal(t, m != ModeNone, Enabled(), "mode=%d", m)
		restore()
	}
}

// --- SetModeForTest restore semantics ---

func TestSetModeForTest_NestedRestore(t *testing.T) {
	// Nest two overrides and make sure restore unwinds in LIFO order.
	r1 := SetModeForTest(ModeTrueColor)
	assert.Equal(t, ModeTrueColor, Detect())

	r2 := SetModeForTest(Mode256)
	assert.Equal(t, Mode256, Detect())

	r2()
	assert.Equal(t, ModeTrueColor, Detect())

	r1()
	// After unwinding both, forcedMode is nil and we fall through to env.
	// In the test environment stdout is not a TTY, so Detect returns None.
	assert.Equal(t, ModeNone, Detect())
}

// --- Out variable swap ---

func TestOut_Default(t *testing.T) {
	// Sanity: the package-level default points at os.Stdout.
	assert.Equal(t, io.Writer(os.Stdout), Out)
}
