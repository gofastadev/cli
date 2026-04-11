package commands

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var consoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Launch an interactive Go REPL (yaegi) in the project directory",
	Long: `Start a yaegi-based Go REPL with your project on the import path. Useful
for poking at services, running ad-hoc queries, or exploring library
APIs without writing a throwaway main().

Yaegi is a third-party Go interpreter, not a CLI dependency — install it
once with:

  go install github.com/traefik/yaegi/cmd/yaegi@latest

The command looks up ` + "`yaegi`" + ` on $PATH and fails fast if it is missing.
Because yaegi is an interpreter (not a compiler), some features (cgo,
generics in older releases, unsafe) may not work — use ` + "`go run`" + ` for
anything yaegi can't handle.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConsole()
	},
}

func init() {
	rootCmd.AddCommand(consoleCmd)
}

func runConsole() error {
	yaegiPath, err := execLookPath("yaegi")
	if err != nil {
		return fmt.Errorf("yaegi is not installed. Install it with:\n  go install github.com/traefik/yaegi/cmd/yaegi@latest")
	}

	fmt.Println("Starting gofasta console (yaegi)...")
	fmt.Println("Type Go code interactively. Press Ctrl+D to exit.")
	fmt.Println()

	cmd := execCommand(yaegiPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
	}()

	return cmd.Run()
}
