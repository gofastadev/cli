package commands

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugReplayMethod    string
	debugReplayPath      string
	debugReplayBody      string
	debugReplayHeaders   []string
	debugReplayStripAuth bool
)

// debugReplayCmd is the user-facing entry for re-firing a captured
// request. Talks to /debug/requests/{id} to fetch the original (so the
// user can see what's about to be replayed) and POST /debug/replay to
// dispatch the re-fire.
//
// Production safety: replay is build-tag-gated on the running app
// (devtools tag must be present). Without devtools, /debug/replay 404s
// and this command surfaces DEBUG_DEVTOOLS_OFF.
var debugReplayCmd = &cobra.Command{
	Use:   "replay <request-id>",
	Short: "Replay a captured request by its ID (id from /debug/requests)",
	Long: `Re-fire a previously captured request against the same app. Useful
for bug triage — find a 500 in /debug/requests, copy its ID, run
` + "`gofasta debug replay <id>`" + ` to reproduce on demand.

The original method, path, headers, and body are pulled from
/debug/requests/{id}. Each may be overridden via flags:

  --method=POST              Replace the HTTP method
  --path=/orders/{id}/refund Replace the path
  --header=K:V               Add/override a header (repeatable)
  --body=@payload.json       Body from file (or "-" for stdin)
  --body='{"k":"v"}'         Body inline
  --strip-auth               Drop Authorization + Cookie headers

SSRF safety: the upstream URL is pinned to the configured app URL by
the skeleton's /debug/replay handler — you cannot redirect a replay to
a different host / scheme / port. Override-rejected requests surface as
DEBUG_REPLAY_UNSAFE.

Examples:

  gofasta debug replay req_42
  gofasta debug replay req_42 --strip-auth
  gofasta debug replay req_42 --path=/orders/{id}/refund --header=Idempotency-Key:abc
  gofasta debug replay req_42 --body=@/tmp/payload.json --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return runDebugReplay(args[0])
	},
}

func init() {
	debugReplayCmd.Flags().StringVar(&debugReplayMethod, "method", "",
		"Override the HTTP method on the replay")
	debugReplayCmd.Flags().StringVar(&debugReplayPath, "path", "",
		"Override the path on the replay")
	debugReplayCmd.Flags().StringVar(&debugReplayBody, "body", "",
		"Override the body (inline, or @file, or - for stdin)")
	debugReplayCmd.Flags().StringSliceVar(&debugReplayHeaders, "header", nil,
		"Add/override a header (repeatable). Format: Key:Value")
	debugReplayCmd.Flags().BoolVar(&debugReplayStripAuth, "strip-auth", false,
		"Drop Authorization + Cookie headers before re-firing")
	debugCmd.AddCommand(debugReplayCmd)
}

// debugRequestEntry is a CLI-side mirror of the skeleton's
// RequestEntry. We don't import the skeleton — we just need enough of
// the shape to display the original and build the replay payload.
type debugRequestEntry struct {
	ID         string              `json:"id"`
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Status     int                 `json:"status"`
	DurationMS int64               `json:"duration_ms"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body,omitempty"`
}

// debugReplayResult mirrors /debug/replay's response payload.
type debugReplayResult struct {
	NewRequestID    string              `json:"new_request_id"`
	Status          int                 `json:"status"`
	DurationMS      int64               `json:"duration_ms"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	ResponseBody    string              `json:"response_body,omitempty"`
}

// debugReplayPayload is the request body for /debug/replay.
type debugReplayPayload struct {
	RequestID string                 `json:"request_id"`
	Overrides debugReplayOverridePld `json:"overrides,omitempty"`
}

type debugReplayOverridePld struct {
	Method    string              `json:"method,omitempty"`
	Path      string              `json:"path,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      string              `json:"body,omitempty"`
	StripAuth bool                `json:"strip_auth,omitempty"`
}

func runDebugReplay(id string) error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}

	// Step 1: fetch the original so we can render it for the user + so
	// we'd error early if the ID was already evicted.
	var original debugRequestEntry
	if err := getJSON(appURL, "/debug/requests/"+url.PathEscape(id), &original); err != nil {
		// /debug/requests/{id} returns 404 with a DEBUG_REPLAY_NOT_FOUND
		// clierr body when the ID is missing; getJSON surfaces that.
		return err
	}

	// Step 2: assemble the override block from the flags.
	headerOverrides, err := parseHeaderFlags(debugReplayHeaders)
	if err != nil {
		return err
	}
	bodyOverride, err := readBodyFlag(debugReplayBody)
	if err != nil {
		return err
	}
	payload := debugReplayPayload{
		RequestID: id,
		Overrides: debugReplayOverridePld{
			Method:    debugReplayMethod,
			Path:      debugReplayPath,
			Headers:   headerOverrides,
			Body:      bodyOverride,
			StripAuth: debugReplayStripAuth,
		},
	}

	// Step 3: fire the replay.
	var result debugReplayResult
	if err := postJSON(appURL, "/debug/replay", payload, &result); err != nil {
		return err
	}

	cliout.Print(result, func(w io.Writer) {
		printReplayText(w, original, payload, result)
	})
	return nil
}

// parseHeaderFlags converts repeated --header=K:V flags into the
// override map. Returns CodeDebugBadFilter when a value is malformed.
func parseHeaderFlags(raw []string) (map[string][]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string][]string, len(raw))
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, ":")
		if !ok {
			return nil, clierr.Newf(clierr.CodeDebugBadFilter,
				"header %q must use Key:Value form", kv)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		out[k] = append(out[k], v)
	}
	return out, nil
}

// readBodyFlag interprets --body — three forms:
//
//	@<path>  → read from path
//	-        → read from stdin
//	<text>   → use as-is
func readBodyFlag(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if raw == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", clierr.Wrap(clierr.CodeFileIO, err, "reading stdin")
		}
		return string(data), nil
	}
	if strings.HasPrefix(raw, "@") {
		data, err := os.ReadFile(strings.TrimPrefix(raw, "@"))
		if err != nil {
			return "", clierr.Wrap(clierr.CodeFileIO, err, "reading body file")
		}
		return string(data), nil
	}
	return raw, nil
}

// printReplayText renders the human-readable view of a replay run.
func printReplayText(w io.Writer, original debugRequestEntry, sent debugReplayPayload, result debugReplayResult) {
	_, _ = fmt.Fprintf(w, "%s %s\n", termcolor.CBrand("Original:"),
		fmt.Sprintf("%s %s → %d (%dms)", original.Method, original.Path, original.Status, original.DurationMS))
	if sent.Overrides.Method != "" || sent.Overrides.Path != "" || len(sent.Overrides.Headers) > 0 || sent.Overrides.Body != "" || sent.Overrides.StripAuth {
		_, _ = fmt.Fprintln(w, termcolor.CDim("  with overrides:"))
		if sent.Overrides.Method != "" {
			_, _ = fmt.Fprintf(w, "    method  → %s\n", sent.Overrides.Method)
		}
		if sent.Overrides.Path != "" {
			_, _ = fmt.Fprintf(w, "    path    → %s\n", sent.Overrides.Path)
		}
		for k, v := range sent.Overrides.Headers {
			_, _ = fmt.Fprintf(w, "    header  → %s: %s\n", k, strings.Join(v, ", "))
		}
		if sent.Overrides.Body != "" {
			_, _ = fmt.Fprintf(w, "    body    → %d byte(s)\n", len(sent.Overrides.Body))
		}
		if sent.Overrides.StripAuth {
			_, _ = fmt.Fprintln(w, "    strip-auth → true")
		}
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", termcolor.CBrand("Replay:"),
		fmt.Sprintf("→ %d (%dms) — new id %s",
			result.Status, result.DurationMS, termcolor.CBold(result.NewRequestID)))
	if result.ResponseBody != "" {
		short := result.ResponseBody
		if len(short) > 400 {
			short = short[:400] + "…"
		}
		_, _ = fmt.Fprintln(w, termcolor.CDim("  response body:"))
		for _, line := range strings.Split(short, "\n") {
			_, _ = fmt.Fprintf(w, "    %s\n", line)
		}
	}
}
