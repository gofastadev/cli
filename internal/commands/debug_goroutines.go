package commands

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugGoroutinesFilter   string
	debugGoroutinesMinCount int
)

// debugGoroutinesCmd fetches the app's goroutine dump (via pprof's
// debug=2 text format, which the devtools handler forwards) and
// groups goroutines by top-of-stack function. Reuses
// parseGoroutineDump from dev_scrape.go so the parsing logic stays
// in one place.
var debugGoroutinesCmd = &cobra.Command{
	Use:   "goroutines",
	Short: "Group live goroutines by top-of-stack function",
	Long: `Dumps /debug/pprof/goroutine?debug=2 from the running app and
aggregates goroutines by the top entry of their stack. Sorted
descending by count so leaks jump to the top.

Examples:

  gofasta debug goroutines
  gofasta debug goroutines --filter=http --min-count=5
  gofasta debug goroutines --json | jq '.groups[0]'`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugGoroutines()
	},
}

func init() {
	debugGoroutinesCmd.Flags().StringVar(&debugGoroutinesFilter, "filter", "",
		"Keep only groups whose top-of-stack contains this substring")
	debugGoroutinesCmd.Flags().IntVar(&debugGoroutinesMinCount, "min-count", 0,
		"Keep only groups with at least this many goroutines")
	debugCmd.AddCommand(debugGoroutinesCmd)
}

func runDebugGoroutines() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	// pprof dumps can be larger than the default 5s client allows under
	// heavy load; allow a generous 15s here.
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(appURL + "/debug/pprof/goroutine?debug=2")
	if err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			"could not fetch goroutine dump")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return clierr.Newf(clierr.CodeDebugAppUnreachable,
			"goroutine dump returned %d", resp.StatusCode)
	}
	buf := make([]byte, 1<<20) // 1 MiB headroom
	n, _ := resp.Body.Read(buf)
	snap := parseGoroutineDump(string(buf[:n]))

	// Apply filters client-side.
	filtered := make([]goroutineGroup, 0, len(snap.Groups))
	for _, g := range snap.Groups {
		if debugGoroutinesFilter != "" && !strings.Contains(g.Top, debugGoroutinesFilter) {
			continue
		}
		if debugGoroutinesMinCount > 0 && g.Count < debugGoroutinesMinCount {
			continue
		}
		filtered = append(filtered, g)
	}
	snap.Groups = filtered

	cliout.Print(snap, func(w io.Writer) {
		fprintf(w, "Total goroutines: %d\n\n", snap.Total)
		if len(filtered) == 0 {
			fprintln(w, "No groups matched the filters.")
			return
		}
		tw := newTabWriter(w)
		fprintln(tw, "COUNT\tSTATES\tTOP")
		for _, g := range filtered {
			states := strings.Join(g.States, ", ")
			if states == "" {
				states = "—"
			}
			fprintf(tw, "%s\t%s\t%s\n",
				termcolor.CBrand(fmt.Sprintf("%d", g.Count)),
				states,
				g.Top,
			)
		}
		_ = tw.Flush()
	})
	return nil
}
