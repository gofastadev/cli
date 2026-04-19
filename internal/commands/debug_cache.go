package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugCacheTrace    string
	debugCacheOp       string
	debugCacheMissOnly bool
	debugCacheLimit    int
)

// debugCacheCmd lists recent cache operations with their hit/miss
// status, duration, and originating trace ID. Aggregate summary
// (total ops, hit rate) is printed as a footer in text mode.
var debugCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "List recent cache operations with hit/miss status",
	Long: `Lists every Get/Set/Delete/Flush/Ping op captured by
devtools.WrapCache. Text output shows a colored hit/miss pill and a
summary footer with hit rate. --json emits the full CacheEntry array
so agents can compute whatever aggregation they need.

Examples:

  gofasta debug cache
  gofasta debug cache --trace=a7f3c8...
  gofasta debug cache --op=get --miss-only
  gofasta debug cache --json | jq '[.[] | select(.op=="get")] | group_by(.hit)'`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugCache()
	},
}

func init() {
	debugCacheCmd.Flags().StringVar(&debugCacheTrace, "trace", "",
		"Filter to ops emitted by this trace ID")
	debugCacheCmd.Flags().StringVar(&debugCacheOp, "op", "",
		"Filter by op — get, set, delete, flush, ping")
	debugCacheCmd.Flags().BoolVar(&debugCacheMissOnly, "miss-only", false,
		"Filter to cache misses (only meaningful for `get` ops)")
	debugCacheCmd.Flags().IntVar(&debugCacheLimit, "limit", 0,
		"Maximum entries to return (0 = all)")
	debugCmd.AddCommand(debugCacheCmd)
}

func runDebugCache() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	var entries []scrapedCache
	if err := getJSON(appURL, "/debug/cache", &entries); err != nil {
		return err
	}
	total := len(entries)

	filtered, err := applyCacheFilters(entries)
	if err != nil {
		return err
	}
	shown := len(filtered)
	if debugCacheLimit > 0 && debugCacheLimit < shown {
		filtered = filtered[:debugCacheLimit]
	}
	filters := map[string]string{
		"trace":     debugCacheTrace,
		"op":        debugCacheOp,
		"miss-only": fmt.Sprintf("%t", debugCacheMissOnly),
	}
	if !debugCacheMissOnly {
		delete(filters, "miss-only")
	}

	cliout.Print(filtered, func(w io.Writer) {
		if len(filtered) == 0 {
			fprintln(w, "No matching cache operations.")
			printFilterSummary(w, 0, total, filters)
			return
		}
		tw := newTabWriter(w)
		fprintln(tw, "TIME\tOP\tKEY\tHIT\tDURATION\tTRACE")
		for _, c := range filtered {
			hit := "—"
			if c.Op == "get" {
				if c.Hit {
					hit = termcolor.CGreen("hit")
				} else {
					hit = termcolor.CYellow("miss")
				}
			}
			fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				formatClock(c.Time),
				c.Op,
				truncate(c.Key, 40),
				hit,
				formatMS(c.DurationMS),
				traceIDShort(c.TraceID),
			)
		}
		_ = tw.Flush()
		hitRate := cacheHitRate(filtered)
		fprintln(w, termcolor.CDim(fmt.Sprintf(
			"\n%d ops · hit rate %.0f%% (among Get ops)",
			len(filtered), hitRate*100,
		)))
		printFilterSummary(w, len(filtered), total, filters)
	})
	return nil
}

// applyCacheFilters narrows the ring entries per flag.
func applyCacheFilters(entries []scrapedCache) ([]scrapedCache, error) {
	want := strings.ToLower(strings.TrimSpace(debugCacheOp))
	if want != "" {
		switch want {
		case "get", "set", "delete", "flush", "ping":
		default:
			return nil, clierr.Newf(clierr.CodeDebugBadFilter,
				"invalid --op %q — accepted: get, set, delete, flush, ping", debugCacheOp)
		}
	}
	out := make([]scrapedCache, 0, len(entries))
	for _, c := range entries {
		if debugCacheTrace != "" && c.TraceID != debugCacheTrace {
			continue
		}
		if want != "" && !strings.EqualFold(c.Op, want) {
			continue
		}
		if debugCacheMissOnly {
			if c.Op != "get" || c.Hit {
				continue
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// cacheHitRate returns hits / (hits + misses) across Get ops.
// Returns 0 when there are no Get ops so callers don't divide by zero.
func cacheHitRate(entries []scrapedCache) float64 {
	var hits, gets int
	for _, c := range entries {
		if c.Op != "get" {
			continue
		}
		gets++
		if c.Hit {
			hits++
		}
	}
	if gets == 0 {
		return 0
	}
	return float64(hits) / float64(gets)
}
