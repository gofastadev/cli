package commands

import (
	"io"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
)

var (
	debugSQLTrace      string
	debugSQLSlowerThan string
	debugSQLContains   string
	debugSQLErrorsOnly bool
	debugSQLLimit      int
)

// debugSQLCmd lists captured SQL statements with bound vars, rows
// affected, duration, and trace ID. Filters mirror debug requests.
var debugSQLCmd = &cobra.Command{
	Use:   "sql",
	Short: "List captured SQL queries (statement, vars, duration, trace ID)",
	Long: `Lists every SQL statement captured by the devtools GORM plugin (up
to 200 entries). Default ordering is newest-first — the same order
the /debug/sql endpoint returns.

Examples:

  gofasta debug sql
  gofasta debug sql --trace=a7f3c8... --json
  gofasta debug sql --slower-than=50ms --limit=20
  gofasta debug sql --contains="FROM users" --errors-only`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugSQL()
	},
}

func init() {
	debugSQLCmd.Flags().StringVar(&debugSQLTrace, "trace", "",
		"Filter to queries emitted by this trace ID")
	debugSQLCmd.Flags().StringVar(&debugSQLSlowerThan, "slower-than", "",
		"Filter to queries exceeding this duration (e.g. 50ms, 1s)")
	debugSQLCmd.Flags().StringVar(&debugSQLContains, "contains", "",
		"Filter to statements containing this substring (case-sensitive)")
	debugSQLCmd.Flags().BoolVar(&debugSQLErrorsOnly, "errors-only", false,
		"Filter to queries that returned an error")
	debugSQLCmd.Flags().IntVar(&debugSQLLimit, "limit", 0,
		"Maximum entries to return (0 = all)")
	debugCmd.AddCommand(debugSQLCmd)
}

func runDebugSQL() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	var entries []scrapedQuery
	if err := getJSON(appURL, "/debug/sql", &entries); err != nil {
		return err
	}
	total := len(entries)
	filtered, err := applySQLFilters(entries)
	if err != nil {
		return err
	}
	shown := len(filtered)
	if debugSQLLimit > 0 && debugSQLLimit < shown {
		filtered = filtered[:debugSQLLimit]
	}

	filters := map[string]string{
		"trace":       debugSQLTrace,
		"slower-than": debugSQLSlowerThan,
		"contains":    debugSQLContains,
	}
	if debugSQLErrorsOnly {
		filters["errors-only"] = "true"
	}

	cliout.Print(filtered, func(w io.Writer) {
		if len(filtered) == 0 {
			fprintln(w, "No matching SQL queries.")
			printFilterSummary(w, 0, total, filters)
			return
		}
		tw := newTabWriter(w)
		fprintln(tw, "TIME\tROWS\tDURATION\tTRACE\tSQL")
		for _, q := range filtered {
			sqlPreview := truncate(oneLine(q.SQL), 70)
			if q.Error != "" {
				sqlPreview = "⚠ " + sqlPreview
			}
			fprintf(tw, "%s\t%d\t%s\t%s\t%s\n",
				formatClock(q.Time),
				q.Rows,
				formatMS(q.DurationMS),
				traceIDShort(q.TraceID),
				sqlPreview,
			)
		}
		_ = tw.Flush()
		printFilterSummary(w, len(filtered), total, filters)
	})
	return nil
}

// applySQLFilters applies each flag-driven filter to the ring entries.
// Extracted so unit tests can exercise filtering without HTTP.
func applySQLFilters(entries []scrapedQuery) ([]scrapedQuery, error) {
	var slowerThan time.Duration
	if debugSQLSlowerThan != "" {
		d, err := time.ParseDuration(debugSQLSlowerThan)
		if err != nil {
			return nil, clierr.Wrapf(clierr.CodeDebugBadDuration, err,
				"invalid --slower-than value %q", debugSQLSlowerThan)
		}
		slowerThan = d
	}
	out := make([]scrapedQuery, 0, len(entries))
	for _, q := range entries {
		if debugSQLTrace != "" && q.TraceID != debugSQLTrace {
			continue
		}
		if debugSQLContains != "" && !strings.Contains(q.SQL, debugSQLContains) {
			continue
		}
		if debugSQLErrorsOnly && q.Error == "" {
			continue
		}
		if slowerThan > 0 &&
			time.Duration(q.DurationMS)*time.Millisecond <= slowerThan {
			continue
		}
		out = append(out, q)
	}
	return out, nil
}

// oneLine collapses any in-SQL newlines + runs of whitespace to a
// single space so table rows stay on one line.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse runs of 2+ spaces to one so reformatted SQL renders
	// cleanly. Loop instead of regexp for zero-import.
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
