package commands

import (
	"fmt"
	"os"

	"github.com/gofastadev/cli/internal/generate"
	"github.com/spf13/cobra"
)

// noBanner is set by the global --no-banner flag. When true, the banner
// is suppressed even if the environment would otherwise show it.
var noBanner bool

// commandsWithoutBanner lists subcommands whose output must stay
// machine-parseable. Showing a decorative banner on top of these would
// break shell-completion scripts, scrape-friendly `--version` output, or
// wrap long-lived streaming commands in noise.
var commandsWithoutBanner = map[string]bool{
	"completion": true,
	"__complete": true, // cobra internal completion helper
}

// shouldSkipBanner returns true when the invocation should not emit the
// banner: explicit opt-out, machine-output commands, or bare --version.
func shouldSkipBanner(cmd *cobra.Command) bool {
	if noBanner {
		return true
	}
	// `gofasta --version` / `gofasta -v` is widely parsed by shell scripts
	// and package managers. Keep its output clean.
	if versionFlag, err := cmd.Flags().GetBool("version"); err == nil && versionFlag {
		return true
	}
	// Walk up the command tree to find the top-level command name, then
	// look it up in the deny list.
	root := cmd
	for root.HasParent() && root.Parent().HasParent() {
		root = root.Parent()
	}
	return commandsWithoutBanner[root.Name()]
}

var rootCmd = &cobra.Command{
	Use:   "gofasta",
	Short: "Gofasta — scaffold and manage Go backend projects built with the Gofasta toolkit",
	Long: `Gofasta is a CLI for scaffolding Go backend projects and generating
idiomatic code on top of the Gofasta library. It creates new projects from
a template, generates resources (models, services, controllers, migrations,
DTOs, Wire providers), runs dev servers and migrations, deploys to remote
hosts, and self-updates.

The CLI is a standalone binary that does not import the Gofasta library —
it only manipulates files on disk.`,
	SilenceUsage: true,
	// PersistentPreRun fires before every Run / RunE, for the root command
	// AND every subcommand in the tree — the exact hook we want for a
	// persistent branded banner.
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		if shouldSkipBanner(cmd) {
			return
		}
		printBanner()
	},
	// Bare `gofasta` (no subcommand, no args) prints banner + help.
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&noBanner, "no-banner", false,
		"Suppress the branded banner shown before each command (also honored via GOFASTA_NO_BANNER=1)")

	// `--help` output bypasses PersistentPreRun — cobra routes help through a
	// dedicated HelpFunc, not the Run chain. Wrap the default so the banner
	// is also shown for `gofasta --help`, `gofasta <cmd> --help`, etc.
	defaultHelpFn := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if !shouldSkipBanner(cmd) {
			printBanner()
		}
		defaultHelpFn(cmd, args)
	})

	rootCmd.AddCommand(generate.Cmd)
	rootCmd.AddCommand(generate.WireCmd)
}

// runExecute is the testable core: runs the root command with the given
// version and returns any error instead of calling os.Exit.
func runExecute(version string) error {
	rootCmd.Version = version
	return rootCmd.Execute()
}

// osExit is a package-level seam so tests can exercise Execute's error
// path without actually terminating the test binary.
var osExit = os.Exit

// Execute runs the root command with the given version string.
func Execute(version string) {
	if err := runExecute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}
