package commands

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the CLI version, Go toolchain version, and OS/arch",
	Long: `Print a multi-line version block useful for bug reports and compatibility
checks. Includes:

  - The gofasta CLI version (from the release tag or runtime/debug)
  - The Go toolchain version the CLI was compiled with
  - The OS/arch pair the running binary was built for

For a single-line, script-friendly version use ` + "`gofasta --version`" + ` instead
— that output is stable and parseable. ` + "`gofasta version`" + ` is intended for
humans and may be expanded in future releases.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVersion()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion() error {
	fmt.Printf("gofasta %s\n", displayVersion(rootCmd.Version))
	fmt.Printf("Go:      %s\n", runtime.Version())
	fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

// displayVersion formats a raw version string for human display. Release
// tags arrive as "v0.1.4" from runtime/debug and should render as-is;
// ldflags-injected tags may or may not carry the "v"; dev builds arrive
// as "dev" or "(devel)". The output always has a leading "v" for
// semver-like strings and is passed through unchanged for anything else.
func displayVersion(v string) string {
	switch v {
	case "", "dev", "(devel)":
		return v
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
