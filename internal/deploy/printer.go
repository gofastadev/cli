package deploy

import (
	"fmt"
	"io"

	"github.com/gofastadev/cli/internal/cliout"
)

// Every printer in this file delegates to cliout.Out() so the deploy
// package shares the same stdout (text mode) / stderr (--json mode)
// routing as the rest of the CLI — keeps the JSON document the
// command emits at the end as the only thing on stdout in --json mode.

// printOut returns the writer all deploy-package output flows through.
// Kept package-private so callers outside deploy can't accidentally
// pick a different writer; ssh.go's RunRemote uses it to redirect a
// child SSH session's stdout to the same destination as our prints.
func printOut() io.Writer { return cliout.Out() }

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

// dprintf is the deploy-package printf used by the multi-step flows
// (binary.go, docker.go, etc.) for inline progress text that doesn't
// fit the Print* verbs above. Routes through the same writer as the
// rest so --json mode stays clean.
func dprintf(format string, args ...any) {
	_, _ = fmt.Fprintf(printOut(), format, args...)
}

// dprintln is the Println sibling of dprintf — same routing.
func dprintln(args ...any) {
	_, _ = fmt.Fprintln(printOut(), args...)
}
