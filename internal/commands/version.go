package commands

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print detailed version information",
	Long:  "Display the CLI version, Go version, and OS/architecture details.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVersion()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion() error {
	fmt.Printf("gofasta v%s\n", rootCmd.Version)
	fmt.Printf("Go:      %s\n", runtime.Version())
	fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}
