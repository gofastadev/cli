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
	Short: "Start an interactive Go REPL with the project loaded",
	Long: `Launch yaegi (Go interpreter) in the current project directory for
interactive exploration of your application code.

Requires yaegi to be installed:
  go install github.com/traefik/yaegi/cmd/yaegi@latest`,
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
			cmd.Process.Signal(os.Interrupt)
		}
	}()

	return cmd.Run()
}
