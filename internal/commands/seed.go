package commands

import (
	"os"

	"github.com/spf13/cobra"
)

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Run every registered seed function against the configured database",
	Long: `Execute every function registered with the project's seeds.Register()
by delegating to ` + "`go run ./app/main seed`" + `. Seeds run in registration order
and are expected to be idempotent (use upserts, not blind inserts).

Use --fresh to drop the schema, re-run migrations, and then seed — the
equivalent of ` + "`gofasta db reset`" + ` with the seed step always enabled. Use
this when you want a known-good fixture state during development.

Must be run from the project root. Reads connection details from
config.yaml via the project binary, not the CLI directly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmdArgs := []string{"run", "./app/main", "seed"}
		fresh, _ := cmd.Flags().GetBool("fresh")
		if fresh {
			cmdArgs = append(cmdArgs, "--fresh")
		}
		c := execCommand("go", cmdArgs...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		return c.Run()
	},
}

func init() {
	seedCmd.Flags().Bool("fresh", false, "Drop tables, re-migrate, then seed")
	rootCmd.AddCommand(seedCmd)
}
