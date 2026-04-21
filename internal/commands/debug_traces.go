package commands

import (
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugTracesSlowerThan string
	debugTracesStatus     string
	debugTracesLimit      int
)

// debugTracesCmd — list command. Summary only; the full waterfall is
// drawn by `gofasta debug trace <id>`.
var debugTracesCmd = &cobra.Command{
	Use:   "traces",
	Short: "List completed traces (root span name, duration, span count, status)",
	Long: `Lists the last 50 completed traces captured by the devtools
SpanProcessor. Summary data only; use ` + "`gofasta debug trace <id>`" + `
for the full waterfall with spans, stacks, and events.

Filters apply to the trace summary; drill-downs return the full trace
unfiltered.

Examples:

  gofasta debug traces
  gofasta debug traces --slower-than=200ms
  gofasta debug traces --status=error --limit=10`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugTracesList()
	},
}

var (
	debugTraceWithStacks bool
)

// debugTraceCmd — single trace drill-down with waterfall rendering.
var debugTraceCmd = &cobra.Command{
	Use:   "trace <id>",
	Short: "Show the full waterfall for a single trace",
	Long: `Fetches /debug/traces/<id> and renders the trace's span tree as
an ASCII waterfall. The ID is the 32-character hex string shown in
the Trace column of ` + "`gofasta debug requests`" + ` — prefix
matching is not supported.

The --with-stacks flag prints each span's captured call stack
inline below the span row. Default is off so the waterfall stays
compact.

JSON output is the full TraceEntry shape (see the scaffold's
app/devtools/devtools.go type declarations) — every span, kind,
status, attribute, event, and 20-frame stack.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDebugTraceDetail(args[0])
	},
}

func init() {
	debugTracesCmd.Flags().StringVar(&debugTracesSlowerThan, "slower-than", "",
		"Filter to traces whose root duration exceeds this value (e.g. 200ms)")
	debugTracesCmd.Flags().StringVar(&debugTracesStatus, "status", "",
		"Filter by trace status — ok or error")
	debugTracesCmd.Flags().IntVar(&debugTracesLimit, "limit", 0,
		"Maximum number of entries to return (0 = all)")
	debugCmd.AddCommand(debugTracesCmd)

	debugTraceCmd.Flags().BoolVar(&debugTraceWithStacks, "with-stacks", false,
		"Print each span's captured call stack inline")
	debugCmd.AddCommand(debugTraceCmd)
}

func runDebugTracesList() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	var entries []scrapedTrace
	if err := getJSON(appURL, "/debug/traces", &entries); err != nil {
		return err
	}
	total := len(entries)

	filtered, err := applyTraceFilters(entries)
	if err != nil {
		return err
	}
	shown := len(filtered)
	if debugTracesLimit > 0 && debugTracesLimit < shown {
		filtered = filtered[:debugTracesLimit]
	}
	filters := map[string]string{
		"slower-than": debugTracesSlowerThan,
		"status":      debugTracesStatus,
	}

	cliout.Print(filtered, func(w io.Writer) {
		if len(filtered) == 0 {
			fprintln(w, "No matching traces.")
			printFilterSummary(w, 0, total, filters)
			return
		}
		tw := newTabWriter(w)
		fprintln(tw, "TIME\tROOT\tSPANS\tDURATION\tSTATUS\tTRACE ID")
		for _, tr := range filtered {
			statusStr := termcolor.CGreen(tr.Status)
			if tr.Status == "error" {
				statusStr = termcolor.CRed(tr.Status)
			}
			fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
				formatClock(tr.Time),
				truncate(tr.RootName, 40),
				tr.SpanCount,
				formatMS(tr.DurationMS),
				statusStr,
				tr.TraceID,
			)
		}
		_ = tw.Flush()
		printFilterSummary(w, len(filtered), total, filters)
	})
	return nil
}

// applyTraceFilters narrows a trace summary list. Extracted for
// unit-testability.
func applyTraceFilters(entries []scrapedTrace) ([]scrapedTrace, error) {
	var slowerThan time.Duration
	if debugTracesSlowerThan != "" {
		d, err := time.ParseDuration(debugTracesSlowerThan)
		if err != nil {
			return nil, clierr.Wrapf(clierr.CodeDebugBadDuration, err,
				"invalid --slower-than value %q", debugTracesSlowerThan)
		}
		slowerThan = d
	}
	want := strings.ToLower(strings.TrimSpace(debugTracesStatus))
	if want != "" && want != "ok" && want != "error" {
		return nil, clierr.Newf(clierr.CodeDebugBadFilter,
			"invalid --status value %q — accepted values: ok, error", debugTracesStatus)
	}
	out := make([]scrapedTrace, 0, len(entries))
	for _, tr := range entries {
		if slowerThan > 0 &&
			time.Duration(tr.DurationMS)*time.Millisecond <= slowerThan {
			continue
		}
		if want != "" && !strings.EqualFold(tr.Status, want) {
			continue
		}
		out = append(out, tr)
	}
	return out, nil
}

// runDebugTraceDetail fetches one trace and renders the waterfall.
func runDebugTraceDetail(id string) error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	// Escape the path segment in case the ID has unusual chars (should
	// never happen — OTel IDs are hex — but being defensive is cheap).
	path := "/debug/traces/" + url.PathEscape(id)
	var trace scrapedTrace
	if err := getJSON(appURL, path, &trace); err != nil {
		return err
	}

	cliout.Print(trace, func(w io.Writer) {
		fprintf(w, "Trace %s · %s · %s · %d spans\n",
			trace.TraceID, trace.RootName,
			formatMS(trace.DurationMS), trace.SpanCount)
		fprintln(w)
		renderWaterfall(w, trace.DurationMS, trace.Spans, debugTraceWithStacks)
	})
	return nil
}
