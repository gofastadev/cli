package commands

import (
	"os"

	"github.com/gofastadev/cli/internal/cliout"
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
		// Load .env so the spawned `go run ./app/main seed` child
		// process sees DB credentials (USER/PASSWORD/NAME) and the
		// host-side port mapping. See migrate.go for the full why —
		// the scaffold keeps these out of config.yaml on purpose,
		// and exec.Cmd inherits os.Environ() by default so values
		// set by loadDotEnv (via os.Setenv) reach the child.
		_, _ = loadDotEnv(".env")

		cmdArgs := []string{"run", "./app/main", "seed"}
		fresh, _ := cmd.Flags().GetBool("fresh")
		if fresh {
			cmdArgs = append(cmdArgs, "--fresh")
		}

		// Announce the step in text mode; in --json mode the child's
		// own output is the contract — wrapping it would corrupt JSON.
		if !cliout.JSON() {
			if fresh {
				cliout.Step("Resetting + seeding database")
			} else {
				cliout.Step("Seeding database")
			}
		}

		c := execCommand("go", cmdArgs...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		err := c.Run()

		if !cliout.JSON() {
			if err != nil {
				cliout.Fail("Seed failed: %s", err.Error())
			} else {
				cliout.Success("Seed complete")
			}
		}
		return err
	},
}

func init() {
	seedCmd.Flags().Bool("fresh", false, "Drop tables, re-migrate, then seed")
	rootCmd.AddCommand(seedCmd)
}
