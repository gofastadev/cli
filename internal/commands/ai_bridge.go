package commands

import (
	"github.com/gofastadev/cli/internal/commands/ai"
)

// init wires the ai subcommand tree into rootCmd and gives ai access to
// the current CLI version string. The bridge lives in the commands
// package (not the ai package) to avoid an import cycle — the ai package
// can't import commands without creating one.
func init() {
	ai.Cmd.GroupID = groupLifecycle
	rootCmd.AddCommand(ai.Cmd)
	ai.SetVersionResolver(func() string { return rootCmd.Version })
}
