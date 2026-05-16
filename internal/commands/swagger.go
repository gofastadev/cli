package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/gofastadev/cli/internal/cliout"
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
automatically.

Use the global ` + "`--json`" + ` flag for machine-readable output: stdout becomes
a single ` + "`{action, output, exit_code, error}`" + ` JSON document so agents can
distinguish success from failure without parsing swag's free-form text.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSwagger()
	},
}

// swaggerResult is the JSON contract for `gofasta swagger --json`. The
// shape is stable: callers parse `action` to know what command ran,
// `exit_code` to detect failure, and `output` for the captured swag
// stdout+stderr (useful for surfacing parse errors to the user).
type swaggerResult struct {
	Action   string `json:"action"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

// runSwagger shells out to `go tool swag init`. In text mode swag's
// stdout+stderr stream straight to the user's terminal so they see
// progress live. In JSON mode the output is captured, the run is
// awaited, and the result is emitted as a single JSON document — the
// agent contract for "did the swag generation succeed?" without
// having to parse swag's freeform text.
func runSwagger() error {
	swag := execCommand("go", "tool", "swag", "init",
		"-g", "app/main/main.go",
		"-o", "docs/",
		"--parseDependency", "--parseInternal",
	)

	if !cliout.JSON() {
		swag.Stdout = os.Stdout
		swag.Stderr = os.Stderr
		return swag.Run()
	}

	var buf bytes.Buffer
	swag.Stdout = &buf
	swag.Stderr = &buf
	runErr := swag.Run()

	result := swaggerResult{
		Action:   "swagger.init",
		Output:   buf.String(),
		ExitCode: exitCodeOf(runErr),
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	cliout.Print(result, func(w io.Writer) {
		// Should be unreachable — JSON branch only — but defensive in
		// case callers pass cliout.Print directly elsewhere.
		_, _ = fmt.Fprintln(w, result.Output)
	})
	return runErr
}

// exitCodeOf returns the numeric exit code for an *exec.ExitError, 0
// for nil, and -1 for any other failure (e.g. couldn't start). Kept
// here (rather than in a shared helper) until a second caller needs it.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	type exitCoder interface{ ExitCode() int }
	if ec, ok := err.(exitCoder); ok {
		return ec.ExitCode()
	}
	return -1
}

func init() {
	rootCmd.AddCommand(swaggerCmd)
}
