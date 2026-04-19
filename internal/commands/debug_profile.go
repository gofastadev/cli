package commands

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugProfileDuration string
	debugProfileOutput   string
)

// debugProfileKinds is the whitelist of pprof profile names the
// devtools handler forwards. Kept explicit so `gofasta debug profile
// blahblah` produces a clean DEBUG_PROFILE_UNSUPPORTED error instead
// of a pprof 404.
var debugProfileKinds = map[string]bool{
	"cpu":          true, // → /debug/pprof/profile (timed capture)
	"heap":         true,
	"goroutine":    true,
	"mutex":        true,
	"block":        true,
	"allocs":       true,
	"threadcreate": true,
	"trace":        true, // execution trace, also timed
}

// debugProfileCmd downloads a pprof profile to a local file (or
// stdout). Thin wrapper — the CLI doesn't parse profiles, just saves
// them for `go tool pprof` consumption. For timed profiles (cpu,
// trace) the --duration flag is forwarded as `seconds=N`.
var debugProfileCmd = &cobra.Command{
	Use:   "profile <kind>",
	Short: "Download a pprof profile from the running app (cpu, heap, goroutine, ...)",
	Long: `Fetches /debug/pprof/<kind> and writes the raw bytes to stdout
or the file given by --output. The returned blob is the standard
Go pprof format; open it with ` + "`go tool pprof <file>`" + ` for
interactive analysis or SVG generation.

Supported kinds: cpu, heap, goroutine, mutex, block, allocs,
threadcreate, trace.

Timed profiles (cpu, trace) accept --duration. Default 30s for cpu,
5s for trace; other kinds ignore it.

Examples:

  gofasta debug profile cpu --duration=30s -o cpu.pprof
  gofasta debug profile heap -o heap.pprof
  gofasta debug profile goroutine > goroutines.pprof
  go tool pprof -http=:8090 cpu.pprof`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDebugProfile(args[0])
	},
}

func init() {
	debugProfileCmd.Flags().StringVar(&debugProfileDuration, "duration", "",
		"Capture duration for timed profiles (cpu, trace). Defaults: 30s cpu, 5s trace.")
	debugProfileCmd.Flags().StringVarP(&debugProfileOutput, "output", "o", "",
		"File to write the profile to (default: stdout)")
	debugCmd.AddCommand(debugProfileCmd)
}

func runDebugProfile(kind string) error {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !debugProfileKinds[kind] {
		return clierr.Newf(clierr.CodeDebugProfileUnsupported,
			"unknown profile kind %q", kind)
	}
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}

	// Map CLI kind → pprof URL path. cpu has a different name in
	// pprof ("profile"); everything else is 1:1.
	path := "/debug/pprof/" + kind
	if kind == "cpu" {
		path = "/debug/pprof/profile"
	}

	// Duration defaults for timed profiles.
	dur, err := resolveProfileDuration(kind, debugProfileDuration)
	if err != nil {
		return err
	}
	if dur > 0 {
		path += fmt.Sprintf("?seconds=%d", int(dur.Seconds()))
	}

	// Use a long-timeout client so a 30s CPU capture doesn't
	// prematurely abort. 5s headroom over the longest allowed
	// duration is plenty.
	client := &http.Client{Timeout: dur + 30*time.Second}
	if dur == 0 {
		client.Timeout = 30 * time.Second
	}
	resp, err := client.Get(appURL + path)
	if err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			"profile fetch failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return clierr.Newf(clierr.CodeDebugAppUnreachable,
			"profile endpoint returned %d", resp.StatusCode)
	}

	var out io.Writer = os.Stdout
	if debugProfileOutput != "" {
		f, err := os.Create(debugProfileOutput)
		if err != nil {
			return clierr.Wrap(clierr.CodeFileIO, err,
				"could not create output file")
		}
		defer func() { _ = f.Close() }()
		out = f
	}

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "profile write failed")
	}
	if debugProfileOutput != "" {
		fprintln(os.Stderr, termcolor.CGreen(fmt.Sprintf(
			"wrote %s (%d bytes) · open with `go tool pprof -http=:8090 %s`",
			debugProfileOutput, n, debugProfileOutput,
		)))
	}
	return nil
}

// resolveProfileDuration returns the capture duration for timed
// profiles. Returns 0 for non-timed profiles.
func resolveProfileDuration(kind, override string) (time.Duration, error) {
	switch kind {
	case "cpu":
		if override == "" {
			return 30 * time.Second, nil
		}
		d, err := time.ParseDuration(override)
		if err != nil {
			return 0, clierr.Wrapf(clierr.CodeDebugBadDuration, err,
				"invalid --duration %q", override)
		}
		return d, nil
	case "trace":
		if override == "" {
			return 5 * time.Second, nil
		}
		d, err := time.ParseDuration(override)
		if err != nil {
			return 0, clierr.Wrapf(clierr.CodeDebugBadDuration, err,
				"invalid --duration %q", override)
		}
		return d, nil
	default:
		return 0, nil
	}
}
