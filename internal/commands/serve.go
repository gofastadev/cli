package commands

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server (delegates to project binary)",
	Long:  "Start the gofasta HTTP server. This delegates to the project's own serve command.",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := exec.Command("go", "run", "./app/main", "serve")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		return c.Run()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
