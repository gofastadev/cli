package commands

import (
	"encoding/json"
	"io"
	"os"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var debugHarOutput string

// debugHarCmd exports the current request ring as HAR 1.2 JSON.
// Reuses buildHAR from dev_dashboard.go so the CLI and the dashboard
// emit identical shapes — importing either file into Chrome
// DevTools, Insomnia, or Postman should produce the same view.
var debugHarCmd = &cobra.Command{
	Use:   "har",
	Short: "Export the request ring as HAR 1.2 JSON",
	Long: `Downloads the last 200 captured requests from /debug/requests and
emits them as HAR 1.2. Redirect to a file or use --output; the
file can then be imported into any HAR-aware viewer:

  - Chrome DevTools  → Network tab → right-click → Import HAR
  - Insomnia         → Import / Export → Import
  - Postman          → Import → HAR
  - har-viewer.dev   → drop the file on the page

Examples:

  gofasta debug har -o session.har
  gofasta debug har > session.har
  gofasta debug har --json | jq '.log.entries[0].request.url'`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugHar()
	},
}

func init() {
	debugHarCmd.Flags().StringVarP(&debugHarOutput, "output", "o", "",
		"File to write the HAR to (default: stdout)")
	debugCmd.AddCommand(debugHarCmd)
}

func runDebugHar() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	var reqs []scrapedRequest
	if err := getJSON(appURL, "/debug/requests", &reqs); err != nil {
		return err
	}
	har := buildHAR(reqs) // shared with dev_dashboard.go

	var out io.Writer = os.Stdout
	if debugHarOutput != "" {
		f, err := os.Create(debugHarOutput)
		if err != nil {
			return clierr.Wrap(clierr.CodeFileIO, err,
				"could not create HAR output file")
		}
		defer func() { _ = f.Close() }()
		out = f
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(har); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "HAR write failed")
	}
	if debugHarOutput != "" {
		fprintln(os.Stderr, termcolor.CGreen(
			"wrote "+debugHarOutput+" · "+intToStr(len(har.Log.Entries))+" entries · import into Chrome DevTools → Network tab",
		))
	}
	return nil
}
