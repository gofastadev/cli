package commands

import (
	"os"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the project's HTTP server (production-mode, no hot reload)",
	Long: `Start the HTTP server by delegating to the project's own binary via
` + "`go run ./app/main serve`" + `. This is the production-mode server — it does
not use Air and does not reload on file changes. For the development
loop with hot reload, use ` + "`gofasta dev`" + ` instead.

The command must be run from the project root. Because it shells out to
the project binary, any cobra flags registered on the project's ` + "`serve`" + `
subcommand are forwarded through unchanged.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load .env so the spawned `go run ./app/main serve` child
		// process inherits DB credentials, REDIS_URL, etc. via
		// os.Environ(). The framework's pkg/config reads from
		// os.Environ() but does NOT load .env on its own (no
		// godotenv import in the skeleton), so the parent must
		// populate the env before exec. See migrate.go for the
		// full rationale.
		_, _ = loadDotEnv(".env")

		c := execCommand("go", "run", "./app/main", "serve")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		return c.Run()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
