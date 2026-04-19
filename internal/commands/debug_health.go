package commands

import (
	"io"
	"net/http"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// debugHealthCmd answers "can I run debug commands against this app
// right now?". It probes /debug/health to determine the devtools tag
// state, then checks every other /debug/* endpoint for a 2xx so
// agents see exactly which surfaces are live.
var debugHealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Probe the running app and report which /debug/* endpoints are reachable",
	Long: `Queries the target app's /debug/health plus each /debug/* endpoint
and reports the results. Useful as a first call — if the devtools
tag isn't set, every other debug command would return 404s; if the
app isn't running, they'd time out. Running health first pinpoints
the real blocker.

The JSON output shape is stable:

  {
    "app_url": "http://localhost:8080",
    "reachable": true,
    "devtools": "enabled" | "stub" | "unreachable",
    "endpoints": [
      {"path": "/debug/requests", "status": 200},
      ...
    ]
  }`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugHealth()
	},
}

func init() {
	debugCmd.AddCommand(debugHealthCmd)
}

// debugHealthReport is the stable JSON contract.
type debugHealthReport struct {
	AppURL    string                `json:"app_url"`
	Reachable bool                  `json:"reachable"`
	Devtools  string                `json:"devtools"`
	Endpoints []debugEndpointStatus `json:"endpoints"`
}

// debugEndpointStatus is one probed endpoint. Status is the HTTP
// status (0 when the request never completed); Error holds a short
// message for unreachable probes.
type debugEndpointStatus struct {
	Path   string `json:"path"`
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

func runDebugHealth() error {
	appURL := resolveAppURL()
	report := debugHealthReport{AppURL: appURL}

	// Probe /debug/health first so we can set Reachable + Devtools.
	probeEndpoint(appURL, "/debug/health", &report)
	healthEntry := report.Endpoints[0]
	report.Reachable = healthEntry.Status >= 200 && healthEntry.Status < 300
	switch {
	case !report.Reachable:
		report.Devtools = "unreachable"
	default:
		// The /debug/health body tells us whether we're in stub mode.
		report.Devtools = readDevtoolsState(appURL)
	}

	// Probe every other endpoint — some (traces, errors) are under
	// /debug/{collection}, some are under /debug/{collection}/{id}.
	// We only probe collection endpoints; the {id} ones 404 without a
	// valid ID so they're not useful as liveness signals.
	for _, path := range []string{
		"/debug/requests",
		"/debug/sql",
		"/debug/traces",
		"/debug/logs",
		"/debug/errors",
		"/debug/cache",
		"/debug/pprof/",
	} {
		probeEndpoint(appURL, path, &report)
	}

	cliout.Print(report, func(w io.Writer) {
		printDebugHealthText(w, report)
	})

	return nil
}

// probeEndpoint issues a short-timeout GET and appends the result to
// the report. Uses the shared debugClient so timeouts are consistent.
func probeEndpoint(appURL, path string, report *debugHealthReport) {
	entry := debugEndpointStatus{Path: path}
	resp, err := debugClient.Get(appURL + path)
	if err != nil {
		entry.Error = err.Error()
		report.Endpoints = append(report.Endpoints, entry)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	entry.Status = resp.StatusCode
	report.Endpoints = append(report.Endpoints, entry)
}

// readDevtoolsState reads /debug/health's JSON body and returns
// "enabled", "stub", or "unreachable".
func readDevtoolsState(appURL string) string {
	resp, err := debugClient.Get(appURL + "/debug/health")
	if err != nil {
		return "unreachable"
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "unreachable"
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	// Body is JSON like {"devtools":"enabled"}. Cheapest correct parse
	// is a substring — this avoids a named type for a two-branch check.
	switch {
	case containsSubstring(body, `"enabled"`):
		return "enabled"
	case containsSubstring(body, `"stub"`):
		return "stub"
	default:
		return "unreachable"
	}
}

// containsSubstring is a tiny helper to avoid pulling in bytes here.
func containsSubstring(haystack []byte, needle string) bool {
	if needle == "" || len(haystack) < len(needle) {
		return false
	}
	n := len(needle)
	for i := 0; i+n <= len(haystack); i++ {
		if string(haystack[i:i+n]) == needle {
			return true
		}
	}
	return false
}

// printDebugHealthText writes the human-readable version of the
// report. Reads cleanly on a terminal and stays under 80 cols.
func printDebugHealthText(w io.Writer, r debugHealthReport) {
	header := termcolor.CBrand("App: ") + r.AppURL
	fprintln(w, header)

	reachBadge := termcolor.CRed("unreachable")
	if r.Reachable {
		reachBadge = termcolor.CGreen("reachable")
	}
	fprintln(w, "Reachable: "+reachBadge)

	devBadge := termcolor.CRed("unreachable")
	switch r.Devtools {
	case "enabled":
		devBadge = termcolor.CGreen("enabled")
	case "stub":
		devBadge = termcolor.CYellow("stub  (production build — rebuild with `gofasta dev`)")
	}
	fprintln(w, "Devtools: "+devBadge)
	fprintln(w)

	tw := newTabWriter(w)
	fprintln(tw, "ENDPOINT\tSTATUS")
	for _, e := range r.Endpoints {
		var status string
		switch {
		case e.Status >= 200 && e.Status < 300:
			status = termcolor.CGreen(numToStr(e.Status) + " OK")
		case e.Status == 0:
			status = termcolor.CRed("unreachable")
		case e.Status == 404:
			status = termcolor.CYellow("404 (endpoint not mounted)")
		default:
			status = termcolor.CYellow(numToStr(e.Status))
		}
		fprintf(tw, "%s\t%s\n", e.Path, status)
	}
	_ = tw.Flush()
}

// numToStr is a tiny helper to avoid importing strconv just for one use.
func numToStr(n int) string {
	// Small numbers only — the endpoint probe returns HTTP status codes
	// which are always 3 digits.
	if n < 0 {
		return "-" + numToStr(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return numToStr(n/10) + string(rune('0'+n%10))
}
