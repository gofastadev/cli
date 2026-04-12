package commands

import (
	"os"

	"github.com/spf13/cobra"
)

var swaggerCmd = &cobra.Command{
	Use:   "swagger",
	Short: "Regenerate OpenAPI/Swagger docs from code annotations",
	Long: `Run ` + "`go tool swag init`" + ` to parse swag-style comment annotations in your
controllers and DTOs and emit an OpenAPI 3 document under docs/. The
generated files are consumed by the project's Swagger UI handler so the
interactive API docs stay in sync with the code.

Entry point is hard-coded to app/main/main.go and output to docs/; run
` + "`go tool swag init --help`" + ` if you need finer control. The ` + "`swag`" + ` tool must
be registered in go.mod — ` + "`gofasta new`" + ` and ` + "`gofasta init`" + ` do this
automatically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		swag := execCommand("go", "tool", "swag", "init",
			"-g", "app/main/main.go",
			"-o", "docs/",
			"--parseDependency", "--parseInternal",
		)
		swag.Stdout = os.Stdout
		swag.Stderr = os.Stderr
		return swag.Run()
	},
}

func init() {
	rootCmd.AddCommand(swaggerCmd)
}
