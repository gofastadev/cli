// Package termcolor is the CLI's shared ANSI styling helper. It exists so
// every command package produces progress output with a consistent palette
// (gofasta brand cyan for steps, green for success, yellow for warnings,
// red for errors, dim for paths) without duplicating TTY / NO_COLOR /
// FORCE_COLOR detection in each package.
//
// All decisions about whether to emit escapes are resolved once per call
// against os.Stdout via Enabled, which honors:
//
//   - NO_COLOR=<anything> disables color entirely (https://no-color.org)
//   - FORCE_COLOR=1      forces truecolor escapes
//   - TTY + COLORTERM    enables truecolor when COLORTERM=truecolor|24bit
//   - TTY alone          enables 256-color
//   - non-TTY            disables color (piped output stays machine-parseable)
//
// Writers can be overridden via Out — tests set it to a bytes.Buffer and
// force Mode via SetModeForTest. The package is intentionally tiny; callers
// should wrap strings with C, CBrand, etc. and let this package decide
// whether the escape is actually emitted.
package termcolor

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Basic 16-color ANSI escapes — supported by every emulator from the last
// three decades. Used for semantic colors (green/yellow/red). The brand
// cyan gets its own 24-bit / 256-color fallback below.
const (
	Reset  = "\x1b[0m"
	Bold   = "\x1b[1m"
	Dim    = "\x1b[2m"
	Green  = "\x1b[32m"
	Yellow = "\x1b[33m"
	Red    = "\x1b[31m"
	Blue   = "\x1b[34m"

	// BrandTrueColor renders gofasta Go Cyan (#00ADD8) via 24-bit escape.
	BrandTrueColor = "\x1b[38;2;0;173;216m"
	// Brand256 is the closest 256-color palette slot (xterm DeepSkyBlue2).
	Brand256 = "\x1b[38;5;38m"
)

// Mode is the resolved color support level for the current output stream.
type Mode int

const (
	// ModeNone — no escapes at all.
	ModeNone Mode = iota
	// Mode256 — basic 16-color and 256-color escapes OK, truecolor falls back.
	Mode256
	// ModeTrueColor — full 24-bit color supported.
	ModeTrueColor
)

// Out is the writer used for TTY detection. Commands that need to style
// output write directly via fmt.Print* to os.Stdout; this var exists so
// tests can swap the target and assert on captured bytes.
var Out io.Writer = os.Stdout

// forcedMode lets tests pin a mode regardless of environment. nil means
// "detect at call time". A stored value short-circuits Enabled() / Detect().
var forcedMode *Mode

// SetModeForTest pins the mode for the duration of a test. Pass a pointer
// to restore. Callers typically defer the restoration.
func SetModeForTest(m Mode) (restore func()) {
	prev := forcedMode
	forcedMode = &m
	return func() { forcedMode = prev }
}

// Detect resolves the mode from environment + the current Out writer. It
// is called from Enabled on every styling operation, which keeps the cost
// (a handful of env reads) proportional to output volume — fine for the
// CLI's print rates.
func Detect() Mode {
	if forcedMode != nil {
		return *forcedMode
	}
	if v := os.Getenv("NO_COLOR"); v != "" {
		return ModeNone
	}
	if v := os.Getenv("FORCE_COLOR"); v != "" && v != "0" {
		return ModeTrueColor
	}
	if !isTTY(Out) {
		return ModeNone
	}
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return ModeTrueColor
	}
	return Mode256
}

// Enabled reports whether any color escapes should be emitted.
func Enabled() bool { return Detect() != ModeNone }

// isTTY reports whether w is an interactive terminal. Non-*os.File writers
// (e.g. bytes.Buffer in tests) return false.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// C wraps s with the given escape code if color is enabled, otherwise
// returns s unchanged. Use the semantic wrappers below instead of calling
// this directly so the call site reads as intent, not formatting.
func C(code, s string) string {
	if !Enabled() {
		return s
	}
	return code + s + Reset
}

// CBold wraps s in a bold ANSI escape when color is enabled.
func CBold(s string) string { return C(Bold, s) }

// CDim wraps s in a dim ANSI escape when color is enabled. Used for paths
// and secondary text that should recede visually.
func CDim(s string) string { return C(Dim, s) }

// CGreen wraps s in a green ANSI escape when color is enabled. Used for
// success markers and completed-operation lines.
func CGreen(s string) string { return C(Green, s) }

// CYellow wraps s in a yellow ANSI escape when color is enabled. Used for
// warnings and non-fatal issues the user should notice.
func CYellow(s string) string { return C(Yellow, s) }

// CRed wraps s in a red ANSI escape when color is enabled. Used for errors
// and failed checks.
func CRed(s string) string { return C(Red, s) }

// CBlue wraps s in a blue ANSI escape when color is enabled. Used for URLs
// and references to external resources.
func CBlue(s string) string { return C(Blue, s) }

// CBrand renders s in gofasta Go Cyan, preferring truecolor and falling
// back to 256-color when the terminal only supports that.
func CBrand(s string) string {
	switch Detect() {
	case ModeTrueColor:
		return BrandTrueColor + s + Reset
	case Mode256:
		return Brand256 + s + Reset
	default:
		return s
	}
}

// --- Pre-composed line printers ---
//
// Every printer writes to stdout via fmt.Println — writers are not overridable
// because the CLI's actual progress output always goes there. Tests either
// pin the mode with SetModeForTest and capture stdout, or assert directly
// on the C*-wrapped strings.

// PrintHeader prints a bold-brand-cyan section header.
func PrintHeader(format string, args ...any) {
	fmt.Println(CBold(CBrand(fmt.Sprintf(format, args...))))
}

// PrintStep prints a brand-cyan progress line, no bolding. Use for sub-
// steps inside a phase ("📦 Installing gofasta library...").
func PrintStep(format string, args ...any) {
	fmt.Println(CBrand(fmt.Sprintf(format, args...)))
}

// PrintSuccess prints a green "✓" followed by the message. Use for
// completed operations and final summaries.
func PrintSuccess(format string, args ...any) {
	fmt.Println(CGreen("✓ ") + fmt.Sprintf(format, args...))
}

// PrintWarn prints a yellow "⚠" followed by the message. Use for non-fatal
// problems the user should see but that don't stop the command.
func PrintWarn(format string, args ...any) {
	fmt.Println(CYellow("⚠ ") + fmt.Sprintf(format, args...))
}

// PrintInfo prints a plain unstyled info line. Included here so all progress
// output flows through a single set of printers, not a mix of fmt.Println
// and termcolor helpers.
func PrintInfo(format string, args ...any) {
	fmt.Println(fmt.Sprintf(format, args...))
}

// PrintPath prints a dim path line with two-space indent. Used when listing
// many files so the tree is scannable without dominating the output.
func PrintPath(path string) {
	fmt.Println("   " + CDim(path))
}

// PrintHint prints a dim indented follow-up — typically a "Try running X"
// line after a warning.
func PrintHint(format string, args ...any) {
	fmt.Println("   " + CDim(fmt.Sprintf(format, args...)))
}

// PrintCreate prints a green "create:" line with a dim path. Used by the
// code generators.
func PrintCreate(path string) {
	fmt.Printf("  %s %s\n", CGreen("create:"), CDim(path))
}

// PrintPatch prints a blue "patch:" line with a dim path + optional note.
func PrintPatch(path, note string) {
	if note == "" {
		fmt.Printf("  %s  %s\n", CBlue("patch:"), CDim(path))
		return
	}
	fmt.Printf("  %s  %s %s\n", CBlue("patch:"), CDim(path), CDim("("+note+")"))
}

// PrintSkip prints a dim "skip:" line with the reason in parentheses.
func PrintSkip(path, reason string) {
	fmt.Printf("  %s   %s %s\n", CDim("skip:"), CDim(path), CDim("("+reason+")"))
}
