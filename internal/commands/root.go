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
	Run: func(cmd *cobra.Command, args []string) {
		printBanner()
		cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(generate.Cmd)
	rootCmd.AddCommand(generate.WireCmd)
}

// runExecute is the testable core: runs the root command with the given
// version and returns any error instead of calling os.Exit.
func runExecute(version string) error {
	rootCmd.Version = version
	return rootCmd.Execute()
}

// Execute runs the root command with the given version string.
func Execute(version string) {
	if err := runExecute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
