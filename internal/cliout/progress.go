package cliout

import (
	"fmt"
	"io"
	"os"

	"github.com/gofastadev/cli/internal/termcolor"
)

// progressOut returns the writer all decorated progress messages flow
// through. In text mode that's stdout (the user reads it as normal CLI
// chatter); in --json mode it's stderr (so the structured JSON document
// the command emits via cliout.Print is the only thing on stdout, while
// humans running with stderr open still see live progress).
//
// Every Step/Success/Warn/Fail/Info/Hint/Header/Blank/Path/Create/Patch
// helper below routes through this single seam, so adding a new global
// mode (say, --quiet) only changes one function.
func progressOut() io.Writer {
	return Out()
}

// Out returns the io.Writer that progress messages currently flow to —
// stdout in text mode, stderr in --json mode. Exposed so other internal
// packages (e.g. deploy.RunRemote which wires a child process's stdout
// to a writer of our choosing) can use the same routing without
// duplicating the JSON-mode check.
func Out() io.Writer {
	if JSON() {
		return os.Stderr
	}
	return os.Stdout
}

// progressDimEnabled tracks whether color codes should be emitted for
// the current writer. Termcolor decides per-writer based on isatty;
// in JSON mode we send to stderr which may or may not be a TTY. The
// termcolor helpers already detect-once at startup, so we just defer
// to whatever they produce — same UX in both modes.

// Header writes a bold-brand-cyan section header.
func Header(format string, args ...any) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Header(format, args...))
}

// Step writes a brand-cyan "▶ " progress line.
func Step(format string, args ...any) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Step(format, args...))
}

// Success writes a green "✓ " decorated message.
func Success(format string, args ...any) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Success(format, args...))
}

// Fail writes a red "✗ " decorated message. Use for failed operations
// and check failures (not for fatal errors — those should be returned
// from RunE so PrintError handles them through the structured path).
func Fail(format string, args ...any) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Fail(format, args...))
}

// Warn writes a yellow "⚠ " decorated message.
func Warn(format string, args ...any) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Warn(format, args...))
}

// Info writes a plain unstyled info line.
func Info(format string, args ...any) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Info(format, args...))
}

// Hint writes a dim indented follow-up line. Use for the actionable
// remediation that follows a Warn or Fail.
func Hint(format string, args ...any) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Hint(format, args...))
}

// Path writes a dim path line with three-space indent. Used by
// generators that list every file they emit.
func Path(path string) {
	_, _ = fmt.Fprintln(progressOut(), termcolor.Path(path))
}

// Create writes a green "create:" line for code generators.
func Create(path string) {
	_, _ = fmt.Fprintf(progressOut(), "  %s %s\n",
		termcolor.CGreen("create:"), termcolor.CDim(path))
}

// Patch writes a blue "patch:" line with an optional parenthetical note.
func Patch(path, note string) {
	if note == "" {
		_, _ = fmt.Fprintf(progressOut(), "  %s  %s\n",
			termcolor.CBlue("patch:"), termcolor.CDim(path))
		return
	}
	_, _ = fmt.Fprintf(progressOut(), "  %s  %s %s\n",
		termcolor.CBlue("patch:"), termcolor.CDim(path),
		termcolor.CDim("("+note+")"))
}

// Skip writes a dim "skip:" line with the reason in parentheses.
func Skip(path, reason string) {
	_, _ = fmt.Fprintf(progressOut(), "  %s   %s %s\n",
		termcolor.CDim("skip:"), termcolor.CDim(path),
		termcolor.CDim("("+reason+")"))
}

// Blank writes one empty line. Use as a visual separator instead of
// fmt.Println() — picks up the same stdout/stderr routing as the rest.
func Blank() {
	_, _ = fmt.Fprintln(progressOut())
}

// Plain writes a fmt.Printf-style line with no decoration. Use only
// when you genuinely need raw text (mostly for `gofasta new`'s
// "next-steps" block where each line is hand-formatted with CBold/CDim).
// Prefer the verb-specific helpers (Step/Info/Hint) when one fits.
func Plain(format string, args ...any) {
	_, _ = fmt.Fprintf(progressOut(), format, args...)
}

// Plainln is Plain + trailing newline.
func Plainln(args ...any) {
	_, _ = fmt.Fprintln(progressOut(), args...)
}
