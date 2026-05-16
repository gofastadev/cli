package deploy

import (
	"fmt"
	"os"

	"github.com/gofastadev/cli/internal/cliout"
)

// In --json mode every Print* below routes to stderr instead of stdout
// so the deploy result the calling command emits via cliout.Print stays
// the only thing on stdout — agents parsing stdout get a single JSON
// document, humans running --json with stderr visible still see live
// progress decorations.
func printOut() *os.File {
	if cliout.JSON() {
		return os.Stderr
	}
	return os.Stdout
}

// PrintStep prints a numbered step message.
func PrintStep(step, total int, msg string) {
	_, _ = fmt.Fprintf(printOut(), "\033[1m==> [%d/%d] %s\033[0m\n", step, total, msg)
}

// PrintSuccess prints a green success message.
func PrintSuccess(msg string) {
	_, _ = fmt.Fprintf(printOut(), "\033[32m✓  %s\033[0m\n", msg)
}

// PrintWarning prints a yellow warning message.
func PrintWarning(msg string) {
	_, _ = fmt.Fprintf(printOut(), "\033[33m⚠  %s\033[0m\n", msg)
}

// PrintError prints a red error message.
func PrintError(msg string) {
	_, _ = fmt.Fprintf(printOut(), "\033[31m✗  %s\033[0m\n", msg)
}

// PrintInfo prints an info message.
func PrintInfo(msg string) {
	_, _ = fmt.Fprintf(printOut(), "   %s\n", msg)
}

// dprintf is a deploy-package internal sibling of fmt.Printf that
// honors --json (writing to stderr instead of stdout). Use it at every
// call site that previously called fmt.Printf for progress chatter, so
// the deploy command's final cliout.Print(...) is the only thing on
// stdout in JSON mode.
func dprintf(format string, args ...any) {
	_, _ = fmt.Fprintf(printOut(), format, args...)
}

// dprintln is the Println sibling of dprintf — same redirect rules.
func dprintln(args ...any) {
	_, _ = fmt.Fprintln(printOut(), args...)
}
