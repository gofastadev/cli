package commands

import (
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
)

// versionInfo is the --json payload. Struct fields are stable API — AI
// agents and scripts consume this, so renaming means a breaking change.
type versionInfo struct {
	Gofasta string `json:"gofasta"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

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
	info := versionInfo{
		Gofasta: displayVersion(rootCmd.Version),
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
	cliout.Print(info, func(w io.Writer) {
		_, _ = fmt.Fprintf(w, "gofasta %s\n", info.Gofasta)
		_, _ = fmt.Fprintf(w, "Go:      %s\n", info.Go)
		_, _ = fmt.Fprintf(w, "OS/Arch: %s/%s\n", info.OS, info.Arch)
	})
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
