package commands

import (
	"io"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
)

var (
	debugExplainVars []string
)

// debugExplainCmd runs an EXPLAIN against a captured SELECT. The
// scaffold's /debug/explain endpoint enforces a SELECT-only whitelist
// — the CLI passes the statement through unchanged.
var debugExplainCmd = &cobra.Command{
	Use:   "explain <sql>",
	Short: "Run EXPLAIN on a captured SELECT via the app's registered *gorm.DB",
	Long: `POSTs the supplied SQL (must start with SELECT) and optional
parameter values to the app's /debug/explain endpoint. The app runs
EXPLAIN against GORM and returns the query plan as plain text.

Quote the SQL argument; --vars accepts one or more bound values in
the order their placeholders appear in the statement.

Examples:

  gofasta debug explain "SELECT * FROM users WHERE id = ?" --vars=42
  gofasta debug explain "SELECT * FROM orders WHERE user_id = ? AND status = ?" \
      --vars=u42 --vars=shipped
  gofasta debug explain "$(gofasta debug sql --limit=1 --json | jq -r '.[0].sql')" \
      --vars="$(gofasta debug sql --limit=1 --json | jq -r '.[0].vars | @csv')"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDebugExplain(args[0])
	},
}

func init() {
	debugExplainCmd.Flags().StringSliceVar(&debugExplainVars, "vars", nil,
		"Parameter values (one --vars per placeholder, or comma-separated)")
	debugCmd.AddCommand(debugExplainCmd)
}

type explainResponse struct {
	Plan string `json:"plan"`
}

func runDebugExplain(sql string) error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(sql)
	if !strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") {
		return clierr.New(clierr.CodeDebugBadFilter,
			"only SELECT statements can be explained; got something else")
	}
	body := map[string]interface{}{
		"sql":  trimmed,
		"vars": debugExplainVars,
	}
	var resp explainResponse
	if err := postJSON(appURL, "/debug/explain", body, &resp); err != nil {
		return err
	}

	cliout.Print(resp, func(w io.Writer) {
		fprintln(w, resp.Plan)
	})
	return nil
}
