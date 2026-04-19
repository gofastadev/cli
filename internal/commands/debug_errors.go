package commands

import (
	"io"
	"strings"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugErrorsLimit    int
	debugErrorsContains string
)

// debugErrorsCmd lists recent exceptions from the /debug/errors ring.
// Each entry carries the recovered value, a 20-frame stack, the
// originating request's method + path, and its trace ID — enough to
// correlate against `gofasta debug trace <id>`.
var debugErrorsCmd = &cobra.Command{
	Use:   "errors",
	Short: "Show recent recovered panics with stacks and originating requests",
	Long: `Lists the last 50 recovered panics captured by
devtools.Recovery. Text mode prints each exception's top line plus
an indented stack; --json emits the full ExceptionEntry array.

Examples:

  gofasta debug errors
  gofasta debug errors --limit=5 --json
  gofasta debug errors --contains="nil pointer"`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugErrors()
	},
}

func init() {
	debugErrorsCmd.Flags().IntVar(&debugErrorsLimit, "limit", 0,
		"Maximum entries to return (0 = all)")
	debugErrorsCmd.Flags().StringVar(&debugErrorsContains, "contains", "",
		"Filter to exceptions whose recovered value contains this substring")
	debugCmd.AddCommand(debugErrorsCmd)
}

func runDebugErrors() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	var entries []scrapedException
	if err := getJSON(appURL, "/debug/errors", &entries); err != nil {
		return err
	}
	total := len(entries)

	filtered := entries
	if debugErrorsContains != "" {
		out := make([]scrapedException, 0, len(entries))
		for _, e := range entries {
			if strings.Contains(e.Recovered, debugErrorsContains) {
				out = append(out, e)
			}
		}
		filtered = out
	}
	shown := len(filtered)
	if debugErrorsLimit > 0 && debugErrorsLimit < shown {
		filtered = filtered[:debugErrorsLimit]
	}
	filters := map[string]string{
		"contains": debugErrorsContains,
	}

	cliout.Print(filtered, func(w io.Writer) {
		if len(filtered) == 0 {
			fprintln(w, "No exceptions recorded.")
			printFilterSummary(w, 0, total, filters)
			return
		}
		for i, e := range filtered {
			if i > 0 {
				fprintln(w)
			}
			head := termcolor.CRed(e.Recovered)
			fprintf(w, "%s  %s %s  %s\n",
				termcolor.CDim(formatClock(e.Time)),
				methodPill(e.Method),
				e.Path,
				head,
			)
			if e.TraceID != "" {
				fprintf(w, "  trace: %s\n", termcolor.CBrand(e.TraceID))
			}
			for _, frame := range e.Stack {
				fprintln(w, termcolor.CDim("  "+frame))
			}
		}
		printFilterSummary(w, len(filtered), total, filters)
	})
	return nil
}
