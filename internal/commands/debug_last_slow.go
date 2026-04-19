package commands

import (
	"io"
	"net/url"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugLastSlowThreshold string
	debugLastSlowWithTrace bool
	debugLastSlowWithLogs  bool
	debugLastSlowWithSQL   bool
	debugLastSlowWithStack bool
)

// debugLastSlowCmd is the composed-diagnostic command for "I noticed
// an endpoint is slow — tell me everything about it." It:
//
//  1. Fetches /debug/requests, filters by --threshold, takes the newest.
//  2. Optionally fetches that request's trace (/debug/traces/{id}).
//  3. Optionally fetches that request's logs (/debug/logs?trace_id=).
//  4. Optionally fetches that request's SQL (all of /debug/sql, filtered client-side).
//  5. Runs detectNPlusOne against the SQL subset.
//
// All four sub-fetches happen over the same client so one slow
// endpoint doesn't drag the others down. The response bundles them
// into one JSON document so agents make one tool call instead of
// four.
var debugLastSlowCmd = &cobra.Command{
	Use:   "last-slow-request",
	Short: "Diagnose the latest request exceeding a duration threshold",
	Long: `Finds the newest captured request whose duration ≥ --threshold
and bundles its trace, logs, SQL, and detected N+1 patterns into
one JSON doc. Designed for "something just broke" agent workflows:
one command returns everything needed to diagnose the incident.

Flags enable/disable each sub-fetch individually (all on by
default except --with-stack, which adds 20-frame stacks per span).

Examples:

  gofasta debug last-slow-request
  gofasta debug last-slow-request --threshold=500ms --json
  gofasta debug last-slow-request --with-stack           # expensive — full stacks
  gofasta debug last-slow-request --json | jq '.n_plus_one'`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugLastSlow()
	},
}

func init() {
	debugLastSlowCmd.Flags().StringVar(&debugLastSlowThreshold, "threshold", "200ms",
		"Minimum request duration to consider slow (e.g. 100ms, 1s)")
	debugLastSlowCmd.Flags().BoolVar(&debugLastSlowWithTrace, "with-trace", true,
		"Include the request's trace waterfall")
	debugLastSlowCmd.Flags().BoolVar(&debugLastSlowWithLogs, "with-logs", true,
		"Include slog records emitted by this request")
	debugLastSlowCmd.Flags().BoolVar(&debugLastSlowWithSQL, "with-sql", true,
		"Include SQL statements issued during this request")
	debugLastSlowCmd.Flags().BoolVar(&debugLastSlowWithStack, "with-stack", false,
		"Include per-span call-stack snapshots (verbose)")
	debugCmd.AddCommand(debugLastSlowCmd)
}

// lastSlowReport is the bundled JSON contract. Any sub-field may be
// nil / empty when the corresponding --with-* flag is false or the
// fetch failed — downstream tooling should tolerate missing fields.
type lastSlowReport struct {
	Threshold string            `json:"threshold"`
	Request   *scrapedRequest   `json:"request"`
	Trace     *scrapedTrace     `json:"trace,omitempty"`
	Logs      []scrapedLog      `json:"logs,omitempty"`
	SQL       []scrapedQuery    `json:"sql,omitempty"`
	NPlusOne  []nPlusOneFinding `json:"n_plus_one,omitempty"`
}

func runDebugLastSlow() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	threshold, err := time.ParseDuration(debugLastSlowThreshold)
	if err != nil {
		return clierr.Wrapf(clierr.CodeDebugBadDuration, err,
			"invalid --threshold value %q", debugLastSlowThreshold)
	}

	picked, totalRequests, err := findLatestSlowRequest(appURL, threshold)
	if err != nil {
		return err
	}
	report := lastSlowReport{Threshold: threshold.String(), Request: picked}
	if picked == nil {
		cliout.Print(report, func(w io.Writer) {
			fprintf(w, "No requests >= %s in the last %d captures.\n",
				threshold, totalRequests)
		})
		return nil
	}

	enrichLastSlowReport(appURL, &report, picked)

	cliout.Print(report, func(w io.Writer) {
		renderLastSlowText(w, report, debugLastSlowWithStack)
	})
	return nil
}

// findLatestSlowRequest fetches the request ring and returns the
// newest request whose duration ≥ threshold. Returns (nil, n, nil)
// when no request matches.
func findLatestSlowRequest(appURL string, threshold time.Duration) (*scrapedRequest, int, error) {
	var requests []scrapedRequest
	if err := getJSON(appURL, "/debug/requests", &requests); err != nil {
		return nil, 0, err
	}
	for i := range requests {
		r := &requests[i]
		if time.Duration(r.DurationMS)*time.Millisecond >= threshold {
			return r, len(requests), nil
		}
	}
	return nil, len(requests), nil
}

// enrichLastSlowReport fans out the optional sub-fetches (trace,
// logs, SQL → N+1) and stuffs them into report. Each fetch failure
// is swallowed — partial data is better than bailing on the whole
// diagnostic.
func enrichLastSlowReport(appURL string, report *lastSlowReport, picked *scrapedRequest) {
	if picked.TraceID == "" {
		return
	}
	if debugLastSlowWithTrace {
		if tr := fetchTrace(appURL, picked.TraceID); tr != nil {
			report.Trace = tr
		}
	}
	if debugLastSlowWithLogs {
		report.Logs = fetchLogsForTrace(appURL, picked.TraceID)
	}
	if debugLastSlowWithSQL {
		report.SQL = fetchSQLForTrace(appURL, picked.TraceID)
		report.NPlusOne = detectNPlusOne(report.SQL)
	}
}

// fetchTrace GETs a single trace. Returns nil on failure so the
// composer gracefully degrades.
func fetchTrace(appURL, traceID string) *scrapedTrace {
	var tr scrapedTrace
	if err := getJSON(appURL, "/debug/traces/"+url.PathEscape(traceID), &tr); err != nil {
		return nil
	}
	return &tr
}

// fetchLogsForTrace returns slog records for the given trace (or
// nil on failure).
func fetchLogsForTrace(appURL, traceID string) []scrapedLog {
	var logs []scrapedLog
	p := appendQuery("/debug/logs", map[string]string{"trace_id": traceID})
	_ = getJSON(appURL, p, &logs)
	return logs
}

// fetchSQLForTrace pulls the full SQL ring and returns only entries
// matching traceID. The scaffold's /debug/sql doesn't support filter
// params so client-side filtering is the cheapest correct approach.
func fetchSQLForTrace(appURL, traceID string) []scrapedQuery {
	var all []scrapedQuery
	if err := getJSON(appURL, "/debug/sql", &all); err != nil {
		return nil
	}
	out := make([]scrapedQuery, 0, len(all))
	for _, q := range all {
		if q.TraceID == traceID {
			out = append(out, q)
		}
	}
	return out
}

// renderLastSlowText prints the human-readable rollup: request
// summary, then each optional section with a colored heading.
func renderLastSlowText(w io.Writer, r lastSlowReport, withStacks bool) {
	h := termcolor.CBrand
	fprintln(w, h("REQUEST"))
	req := r.Request
	fprintf(w, "  %s  %s %s → %s  %s  trace=%s\n",
		formatClock(req.Time),
		methodPill(req.Method),
		req.Path,
		statusPill(req.Status),
		formatMS(req.DurationMS),
		req.TraceID,
	)

	if r.Trace != nil {
		fprintln(w)
		fprintln(w, h("TRACE"))
		renderWaterfall(w, r.Trace.DurationMS, r.Trace.Spans, withStacks)
	}
	if len(r.NPlusOne) > 0 {
		fprintln(w)
		fprintln(w, h("N+1"))
		for _, f := range r.NPlusOne {
			fprintf(w, "  %s× %s\n",
				termcolor.CRed(intToStr(f.Count)),
				truncate(f.Template, 80),
			)
		}
	}
	if len(r.SQL) > 0 {
		fprintln(w)
		fprintln(w, h("SQL"))
		tw := newTabWriter(w)
		fprintln(tw, "  DURATION\tROWS\tSTATEMENT")
		for _, q := range r.SQL {
			fprintf(tw, "  %s\t%d\t%s\n",
				formatMS(q.DurationMS),
				q.Rows,
				truncate(oneLine(q.SQL), 70),
			)
		}
		_ = tw.Flush()
	}
	if len(r.Logs) > 0 {
		fprintln(w)
		fprintln(w, h("LOGS"))
		for _, l := range r.Logs {
			fprintf(w, "  %s  %s  %s%s\n",
				termcolor.CDim(formatClock(l.Time)),
				levelPill(padLevel(l.Level)),
				l.Message,
				formatAttrs(l.Attrs),
			)
		}
	}
}

// intToStr keeps the composer free of strconv for a single digit-to-
// string use (the fast path most render helpers already follow).
func intToStr(n int) string {
	if n < 0 {
		return "-" + intToStr(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return intToStr(n/10) + string(rune('0'+n%10))
}
