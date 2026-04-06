package commands

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Run database seed functions (delegates to project binary)",
	Long:  "Seed the database with sample data. This delegates to the project's own seed command.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmdArgs := []string{"run", "./app/main", "seed"}
		fresh, _ := cmd.Flags().GetBool("fresh")
		if fresh {
			cmdArgs = append(cmdArgs, "--fresh")
		}
		c := exec.Command("go", cmdArgs...)
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
