package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugLogsTrace    string
	debugLogsLevel    string
	debugLogsContains string
)

// debugLogsCmd streams slog records from the devtools log ring. Unlike
// the other list commands, --trace is STRONGLY recommended — without
// it the command returns every buffered log line, which is rarely
// what an agent wants. The scaffold's /debug/logs endpoint itself
// honors the filter; we pass it through server-side and never
// download the full ring.
var debugLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show slog records captured for a trace (message, level, attrs)",
	Long: `Fetches slog records captured by devtools.WrapLogger, optionally
filtered server-side by trace ID and minimum level. Attrs are
printed in a compact key=value form; full JSON is available with
--json.

At least one of --trace or --level must usually be set — otherwise
the command pulls every buffered log line (useful for a fresh
session but noisy in a live dev loop).

Examples:

  gofasta debug logs --trace=a7f3c8...
  gofasta debug logs --trace=a7f3c8... --level=WARN
  gofasta debug logs --trace=a7f3c8... --contains="cache miss"`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugLogs()
	},
}

func init() {
	debugLogsCmd.Flags().StringVar(&debugLogsTrace, "trace", "",
		"Filter to logs with this trace ID (forwarded to /debug/logs)")
	debugLogsCmd.Flags().StringVar(&debugLogsLevel, "level", "",
		"Minimum log level — DEBUG, INFO, WARN, ERROR")
	debugLogsCmd.Flags().StringVar(&debugLogsContains, "contains", "",
		"Filter to messages containing this substring (client-side)")
	debugCmd.AddCommand(debugLogsCmd)
}

func runDebugLogs() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	path := appendQuery("/debug/logs", map[string]string{
		"trace_id": debugLogsTrace,
		"level":    debugLogsLevel,
	})
	var entries []scrapedLog
	if err := getJSON(appURL, path, &entries); err != nil {
		return err
	}
	total := len(entries)

	// Client-side --contains filter; server only honors trace + level.
	if debugLogsContains != "" {
		filtered := make([]scrapedLog, 0, len(entries))
		for _, e := range entries {
			if strings.Contains(e.Message, debugLogsContains) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	filters := map[string]string{
		"trace":    debugLogsTrace,
		"level":    debugLogsLevel,
		"contains": debugLogsContains,
	}

	cliout.Print(entries, func(w io.Writer) {
		if len(entries) == 0 {
			fprintln(w, "No matching log records.")
			printFilterSummary(w, 0, total, filters)
			return
		}
		for _, e := range entries {
			fprintf(w, "%s  %s  %s%s\n",
				termcolor.CDim(formatClock(e.Time)),
				levelPill(padLevel(e.Level)),
				e.Message,
				formatAttrs(e.Attrs),
			)
		}
		printFilterSummary(w, len(entries), total, filters)
	})
	return nil
}

// padLevel right-pads a level string to 5 chars so the message column
// lines up vertically across rows (INFO → "INFO ", ERROR → "ERROR").
func padLevel(level string) string {
	const width = 5
	if len(level) >= width {
		return level[:width]
	}
	return level + strings.Repeat(" ", width-len(level))
}

// formatAttrs renders structured log attributes as a compact
// {key=value, key=value} suffix. Returns "" for the empty map so
// attr-less records don't trail whitespace. Keys are sorted so the
// output is deterministic across runs.
func formatAttrs(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(attrs))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, attrs[k]))
	}
	return termcolor.CDim(" {" + strings.Join(parts, ", ") + "}")
}
