package termcolor

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns the
// captured output. Used to assert on what the Print* helpers actually emit.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-done
}

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

func TestPrintHelpers(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()

	out := captureStdout(t, func() {
		PrintHeader("h %s", "one")
		PrintStep("s %s", "two")
		PrintSuccess("ok %s", "done")
		PrintWarn("warn %s", "now")
		PrintInfo("info %s", "msg")
		PrintPath("a/b/c")
		PrintHint("try %s", "it")
		PrintCreate("new/file.go")
		PrintPatch("old/file.go", "")
		PrintPatch("old/file.go", "note")
		PrintSkip("x.go", "exists")
	})

	for _, want := range []string{
		"h one", "s two", "✓ ok done", "⚠ warn now",
		"info msg", "a/b/c", "try it",
		"create:", "new/file.go",
		"patch:", "old/file.go", "note",
		"skip:", "x.go", "exists",
	} {
		assert.True(t, strings.Contains(out, want), "output missing %q:\n%s", want, out)
	}
}

func TestPrintHelpers_Colored(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()

	out := captureStdout(t, func() {
		PrintSuccess("done")
		PrintWarn("watch out")
		PrintCreate("x")
	})
	// With color on, the success line should contain the green escape.
	assert.Contains(t, out, Green)
	assert.Contains(t, out, Yellow)
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

// --- Format-arg printing ---
//
// The Print* helpers all take (format, args...). Make sure they format
// correctly and don't mangle % escapes.

func TestPrintFormatting(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()

	got := captureStdout(t, func() {
		PrintHeader("%s-%d", "phase", 1)
		PrintStep("step=%s", "build")
		PrintSuccess("done in %dms", 42)
		PrintWarn("%d errors", 3)
		PrintInfo("info=%v", true)
		PrintHint("try %q", "gofasta")
	})

	expected := []string{
		"phase-1",
		"step=build",
		"done in 42ms",
		"3 errors",
		"info=true",
		`try "gofasta"`,
	}
	for _, want := range expected {
		assert.Contains(t, got, want)
	}
}

func TestPrintCreate_PathOnly(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()
	got := captureStdout(t, func() {
		PrintCreate("db/migrations/001_create_users.up.sql")
	})
	assert.Contains(t, got, "create:")
	assert.Contains(t, got, "db/migrations/001_create_users.up.sql")
}

func TestPrintPatch_BothShapes(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()

	plain := captureStdout(t, func() { PrintPatch("app/di/wire.go", "") })
	noted := captureStdout(t, func() { PrintPatch("app/di/wire.go", "provider set") })

	assert.Contains(t, plain, "app/di/wire.go")
	assert.NotContains(t, plain, "(")
	assert.Contains(t, noted, "(provider set)")
}

func TestPrintSkip(t *testing.T) {
	restore := SetModeForTest(ModeNone)
	defer restore()
	got := captureStdout(t, func() {
		PrintSkip("app/services/user.service.go", "exists")
	})
	assert.Contains(t, got, "skip:")
	assert.Contains(t, got, "app/services/user.service.go")
	assert.Contains(t, got, "(exists)")
}

// --- Color-off vs color-on output shape ---

func TestPrintSuccess_ColorOnContainsGreenResetWrap(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintSuccess("ok") })
	// Success wraps the ✓ in green, then appends the message.
	assert.Contains(t, got, Green+"✓ "+Reset)
	assert.Contains(t, got, "ok")
}

func TestPrintWarn_ColorOnContainsYellowResetWrap(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintWarn("watch out") })
	assert.Contains(t, got, Yellow+"⚠ "+Reset)
}

func TestPrintHeader_ColorOnHasBoldBrandWrap(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintHeader("Section") })
	// Header applies bold *and* brand truecolor — both escapes must appear.
	assert.Contains(t, got, Bold)
	assert.Contains(t, got, BrandTrueColor)
	assert.Contains(t, got, "Section")
}

func TestPrintStep_ColorOn256(t *testing.T) {
	restore := SetModeForTest(Mode256)
	defer restore()
	got := captureStdout(t, func() { PrintStep("go mod tidy") })
	// Mode256 uses the 256-color fallback, not truecolor.
	assert.Contains(t, got, Brand256)
	assert.NotContains(t, got, BrandTrueColor)
}

func TestPrintCreate_ColorOn(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintCreate("x.go") })
	// "create:" is green, path is dim.
	assert.Contains(t, got, Green+"create:"+Reset)
	assert.Contains(t, got, Dim+"x.go"+Reset)
}

func TestPrintPatch_ColorOn(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintPatch("x.go", "hint") })
	assert.Contains(t, got, Blue+"patch:"+Reset)
	assert.Contains(t, got, Dim+"x.go"+Reset)
	assert.Contains(t, got, Dim+"(hint)"+Reset)
}

func TestPrintSkip_ColorOn(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintSkip("x.go", "exists") })
	// Everything in skip lines is dim.
	assert.Contains(t, got, Dim+"skip:"+Reset)
	assert.Contains(t, got, Dim+"x.go"+Reset)
	assert.Contains(t, got, Dim+"(exists)"+Reset)
}

func TestPrintPath_ColorOn(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintPath("app/models/user.go") })
	assert.Contains(t, got, "   ")
	assert.Contains(t, got, Dim+"app/models/user.go"+Reset)
}

func TestPrintHint_ColorOn(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintHint("run %s", "gofasta init") })
	assert.Contains(t, got, "   ")
	assert.Contains(t, got, Dim+"run gofasta init"+Reset)
}

func TestPrintInfo_NeverColored(t *testing.T) {
	restore := SetModeForTest(ModeTrueColor)
	defer restore()
	got := captureStdout(t, func() { PrintInfo("just a note") })
	assert.Equal(t, "just a note\n", got)
	// PrintInfo must never emit escapes even when color is on.
	assert.NotContains(t, got, "\x1b[")
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
