package commands

import (
	"fmt"
	"io"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// debugNPlusOneCmd reuses the existing detectNPlusOne function from
// dev_scrape.go: fetch the SQL ring, group by (trace, normalized SQL
// template), report any group with >= 3 hits.
var debugNPlusOneCmd = &cobra.Command{
	Use:   "n-plus-one",
	Short: "Detect N+1 query patterns in recently captured SQL",
	Long: `Groups the devtools SQL ring by (trace_id, normalized SQL
template) and flags any trace where the same template fires 3 or
more times. Template normalization replaces string / numeric
literals with ? and collapses whitespace — so queries differing
only in parameters collapse into one finding.

Empty output means no N+1 patterns in the last 200 SQL captures,
not that your codebase is clean — the ring evicts quickly under
load.

Examples:

  gofasta debug n-plus-one
  gofasta debug n-plus-one --json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugNPlusOne()
	},
}

func init() {
	debugCmd.AddCommand(debugNPlusOneCmd)
}

func runDebugNPlusOne() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	var queries []scrapedQuery
	if err := getJSON(appURL, "/debug/sql", &queries); err != nil {
		return err
	}
	findings := detectNPlusOne(queries)

	cliout.Print(findings, func(w io.Writer) {
		if len(findings) == 0 {
			fprintln(w, "No N+1 patterns detected in the last 200 SQL captures.")
			return
		}
		tw := newTabWriter(w)
		fprintln(tw, "COUNT\tTRACE\tTEMPLATE")
		for _, f := range findings {
			fprintf(tw, "%s\t%s\t%s\n",
				termcolor.CRed(fmt.Sprintf("%d×", f.Count)),
				traceIDShort(f.TraceID),
				truncate(f.Template, 80),
			)
		}
		_ = tw.Flush()
	})
	return nil
}
