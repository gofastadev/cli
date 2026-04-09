package commands

import (
	"fmt"
	"os"

	"github.com/gofastadev/cli/internal/generate"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gofasta",
	Short: "Gofasta - A scalable Go web framework CLI",
	Long:  "Gofasta CLI tool for scaffolding, generating code, and managing gofasta projects.",
}

func init() {
	rootCmd.AddCommand(generate.Cmd)
	rootCmd.AddCommand(generate.WireCmd)
}

// Execute runs the root command with the given version string.
func Execute(version string) {
	rootCmd.Version = version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
