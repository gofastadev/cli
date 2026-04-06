package commands

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var swaggerCmd = &cobra.Command{
	Use:   "swagger",
	Short: "Generate Swagger/OpenAPI documentation",
	Long:  "Runs swag init to generate OpenAPI docs from code annotations.",
	RunE: func(cmd *cobra.Command, args []string) error {
		swag := exec.Command("go", "tool", "swag", "init", "-g", "app/main/main.go", "-o", "docs/")
		swag.Stdout = os.Stdout
		swag.Stderr = os.Stderr
		return swag.Run()
	},
}

func init() {
	rootCmd.AddCommand(swaggerCmd)
}
