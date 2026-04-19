package commands

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
)

var (
	debugRequestsTrace      string
	debugRequestsMethod     string
	debugRequestsStatus     string
	debugRequestsPath       string
	debugRequestsSlowerThan string
	debugRequestsLimit      int
)

// debugRequestsCmd lists recent captured requests, optionally filtered.
// Filters are applied client-side after the endpoint returns — the
// scaffold's /debug/requests doesn't accept query params.
var debugRequestsCmd = &cobra.Command{
	Use:   "requests",
	Short: "List recent captured requests (method, path, status, duration, trace ID)",
	Long: `Lists every request captured by the devtools middleware (up to 200
entries — older ones evict). All filters are additive. --json emits
a single JSON array; text output is a tabwriter'd table.

Examples:

  gofasta debug requests
  gofasta debug requests --slower-than=100ms
  gofasta debug requests --status=5xx --limit=5
  gofasta debug requests --trace=a7f3c8...
  gofasta debug requests --method=POST --path=/api/v1/orders --json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugRequests()
	},
}

func init() {
	debugRequestsCmd.Flags().StringVar(&debugRequestsTrace, "trace", "",
		"Filter to requests with this trace ID")
	debugRequestsCmd.Flags().StringVar(&debugRequestsMethod, "method", "",
		"Filter by HTTP method (case-insensitive)")
	debugRequestsCmd.Flags().StringVar(&debugRequestsStatus, "status", "",
		"Filter by status code or class — 200, 201, 2xx, 4xx, 5xx, 200-299")
	debugRequestsCmd.Flags().StringVar(&debugRequestsPath, "path", "",
		"Filter to paths containing this substring")
	debugRequestsCmd.Flags().StringVar(&debugRequestsSlowerThan, "slower-than", "",
		"Filter to requests whose duration exceeds this value (e.g. 100ms, 1s)")
	debugRequestsCmd.Flags().IntVar(&debugRequestsLimit, "limit", 0,
		"Maximum number of entries to return (0 = all)")
	debugCmd.AddCommand(debugRequestsCmd)
}

func runDebugRequests() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}

	var entries []scrapedRequest
	if err := getJSON(appURL, "/debug/requests", &entries); err != nil {
		return err
	}
	total := len(entries)

	// Apply each filter in sequence. Keeping these as separate passes
	// is O(n) each but the ring caps at 200 entries — clarity beats
	// micro-optimization here.
	filters := map[string]string{
		"trace":       debugRequestsTrace,
		"method":      debugRequestsMethod,
		"status":      debugRequestsStatus,
		"path":        debugRequestsPath,
		"slower-than": debugRequestsSlowerThan,
	}

	filtered, err := applyRequestFilters(entries)
	if err != nil {
		return err
	}
	shown := len(filtered)
	if debugRequestsLimit > 0 && debugRequestsLimit < shown {
		filtered = filtered[:debugRequestsLimit]
	}

	cliout.Print(filtered, func(w io.Writer) {
		if len(filtered) == 0 {
			fprintln(w, "No matching requests.")
			printFilterSummary(w, 0, total, filters)
			return
		}
		tw := newTabWriter(w)
		fprintln(tw, "TIME\tMETHOD\tPATH\tSTATUS\tDURATION\tTRACE")
		for _, r := range filtered {
			fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				formatClock(r.Time),
				methodPill(r.Method),
				truncate(r.Path, 50),
				statusPill(r.Status),
				formatMS(r.DurationMS),
				traceIDShort(r.TraceID),
			)
		}
		_ = tw.Flush()
		printFilterSummary(w, len(filtered), total, filters)
	})
	return nil
}

// applyRequestFilters applies every flag-driven filter to the raw ring.
// Extracted so tests can verify filter logic without a running HTTP
// server.
func applyRequestFilters(entries []scrapedRequest) ([]scrapedRequest, error) {
	f, err := compileRequestFilters()
	if err != nil {
		return nil, err
	}
	out := make([]scrapedRequest, 0, len(entries))
	for _, r := range entries {
		if f.matches(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// requestFilter is the pre-parsed filter bag. Splitting parse from
// apply keeps the hot loop flat and the dispatcher readable.
type requestFilter struct {
	trace      string
	method     string
	path       string
	slowerThan time.Duration
	statusMin  int
	statusMax  int
}

func compileRequestFilters() (requestFilter, error) {
	var f requestFilter
	if debugRequestsSlowerThan != "" {
		d, err := time.ParseDuration(debugRequestsSlowerThan)
		if err != nil {
			return f, clierr.Wrapf(clierr.CodeDebugBadDuration, err,
				"invalid --slower-than value %q", debugRequestsSlowerThan)
		}
		f.slowerThan = d
	}
	lo, hi, err := parseStatusRange(debugRequestsStatus)
	if err != nil {
		return f, err
	}
	f.statusMin, f.statusMax = lo, hi
	f.trace = debugRequestsTrace
	f.method = debugRequestsMethod
	f.path = debugRequestsPath
	return f, nil
}

func (f requestFilter) matches(r scrapedRequest) bool {
	if f.trace != "" && r.TraceID != f.trace {
		return false
	}
	if f.method != "" && !strings.EqualFold(r.Method, f.method) {
		return false
	}
	if f.path != "" && !strings.Contains(r.Path, f.path) {
		return false
	}
	if f.slowerThan > 0 &&
		time.Duration(r.DurationMS)*time.Millisecond <= f.slowerThan {
		return false
	}
	if f.statusMax > 0 && (r.Status < f.statusMin || r.Status > f.statusMax) {
		return false
	}
	return true
}

// parseStatusRange accepts "200", "2xx", "4xx", "200-299", "200,201".
// Returns an inclusive (lo, hi) range. Empty input returns (0, 0).
// The per-syntax parsing is delegated to helpers so this dispatcher
// stays under the cyclomatic-complexity threshold.
func parseStatusRange(s string) (lo, hi int, err error) {
	if s == "" {
		return 0, 0, nil
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if lo, hi, ok := parseStatusClass(s); ok {
		return lo, hi, nil
	}
	if lo, hi, ok, perr := parseStatusExplicitRange(s); ok {
		return lo, hi, perr
	}
	if lo, hi, ok, perr := parseStatusCommaList(s); ok {
		return lo, hi, perr
	}
	v, perr := parseInt(s)
	if perr != nil {
		return 0, 0, clierr.Newf(clierr.CodeDebugBadFilter,
			"invalid status %q", s)
	}
	return v, v, nil
}

// parseStatusClass matches "2xx" / "5xx" / etc. ok=false means "this
// input isn't a class string, try the next parser".
func parseStatusClass(s string) (lo, hi int, ok bool) {
	if len(s) != 3 || s[1] != 'x' || s[2] != 'x' {
		return 0, 0, false
	}
	digit := int(s[0] - '0')
	if digit < 1 || digit > 5 {
		// Malformed class like "6xx" — fall through so the
		// dispatcher produces the generic "invalid" error.
		return 0, 0, false
	}
	return digit * 100, digit*100 + 99, true
}

// parseStatusExplicitRange matches "200-299". ok=true means the input
// looked like a range (even if invalid); err is authoritative when ok.
func parseStatusExplicitRange(s string) (lo, hi int, ok bool, err error) {
	i := strings.IndexByte(s, '-')
	if i <= 0 {
		return 0, 0, false, nil
	}
	a, err1 := parseInt(s[:i])
	b, err2 := parseInt(s[i+1:])
	if err1 != nil || err2 != nil {
		return 0, 0, true, clierr.Newf(clierr.CodeDebugBadFilter,
			"invalid status range %q", s)
	}
	return a, b, true, nil
}

// parseStatusCommaList matches "200,201,500". Collapses to the (min,
// max) span across the listed codes.
func parseStatusCommaList(s string) (lo, hi int, ok bool, err error) {
	if !strings.Contains(s, ",") {
		return 0, 0, false, nil
	}
	parts := strings.Split(s, ",")
	lo, hi = -1, -1
	for _, p := range parts {
		v, perr := parseInt(strings.TrimSpace(p))
		if perr != nil {
			return 0, 0, true, clierr.Newf(clierr.CodeDebugBadFilter,
				"invalid status code %q", p)
		}
		if lo < 0 || v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi, true, nil
}

// parseInt is a tiny wrapper to keep the status parser from importing
// strconv just for one call. Returns err for any non-digit input.
func parseInt(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-digit")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
