package commands

import (
	"io"
	"net/url"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugLastErrorWithTrace bool
	debugLastErrorWithLogs  bool
)

// debugLastErrorCmd is the "show me the most recent panic with full
// context" composed diagnostic. Bundles the exception + its trace +
// its logs so an agent landing on a panicking endpoint has
// everything in one call.
var debugLastErrorCmd = &cobra.Command{
	Use:   "last-error",
	Short: "Show the most recent recovered panic with surrounding context",
	Long: `Fetches the newest entry from /debug/errors and bundles it with
the offending request's trace (if trace ID was captured) and log
records. The composite JSON document is the agent's single tool
call for incident triage.

Examples:

  gofasta debug last-error
  gofasta debug last-error --json | jq '.exception.recovered'
  gofasta debug last-error --with-trace=false   # skip trace fetch`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugLastError()
	},
}

func init() {
	debugLastErrorCmd.Flags().BoolVar(&debugLastErrorWithTrace, "with-trace", true,
		"Include the exception's trace waterfall")
	debugLastErrorCmd.Flags().BoolVar(&debugLastErrorWithLogs, "with-logs", true,
		"Include slog records emitted by the failing request")
	debugCmd.AddCommand(debugLastErrorCmd)
}

// lastErrorReport is the bundled JSON contract.
type lastErrorReport struct {
	Exception *scrapedException `json:"exception"`
	Trace     *scrapedTrace     `json:"trace,omitempty"`
	Logs      []scrapedLog      `json:"logs,omitempty"`
}

func runDebugLastError() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	var exceptions []scrapedException
	if err := getJSON(appURL, "/debug/errors", &exceptions); err != nil {
		return err
	}
	var report lastErrorReport
	if len(exceptions) == 0 {
		cliout.Print(report, func(w io.Writer) {
			fprintln(w, "No exceptions recorded this session.")
		})
		return nil
	}
	report.Exception = &exceptions[0]

	if debugLastErrorWithTrace && report.Exception.TraceID != "" {
		var tr scrapedTrace
		if err := getJSON(appURL, "/debug/traces/"+url.PathEscape(report.Exception.TraceID), &tr); err == nil {
			report.Trace = &tr
		}
	}
	if debugLastErrorWithLogs && report.Exception.TraceID != "" {
		var logs []scrapedLog
		p := appendQuery("/debug/logs", map[string]string{"trace_id": report.Exception.TraceID})
		if err := getJSON(appURL, p, &logs); err == nil {
			report.Logs = logs
		}
	}

	cliout.Print(report, func(w io.Writer) {
		h := termcolor.CBrand
		e := report.Exception
		fprintln(w, h("EXCEPTION"))
		fprintf(w, "  %s  %s %s  trace=%s\n",
			formatClock(e.Time),
			methodPill(e.Method),
			e.Path,
			e.TraceID,
		)
		fprintf(w, "  %s\n", termcolor.CRed(e.Recovered))
		for _, frame := range e.Stack {
			fprintln(w, termcolor.CDim("    "+frame))
		}
		if report.Trace != nil {
			fprintln(w)
			fprintln(w, h("TRACE"))
			renderWaterfall(w, report.Trace.DurationMS, report.Trace.Spans, false)
		}
		if len(report.Logs) > 0 {
			fprintln(w)
			fprintln(w, h("LOGS"))
			for _, l := range report.Logs {
				fprintf(w, "  %s  %s  %s%s\n",
					termcolor.CDim(formatClock(l.Time)),
					levelPill(padLevel(l.Level)),
					l.Message,
					formatAttrs(l.Attrs),
				)
			}
		}
	})
	return nil
}
