package commands

import (
	"github.com/spf13/cobra"
)

// debugCmd is the root `gofasta debug` command. It groups every agent-
// and human-facing query into the running app's /debug/* endpoints —
// requests, SQL, traces, logs, errors, cache ops, goroutines, pprof,
// HAR export, EXPLAIN, and the composed diagnostics (last-slow-request,
// last-error, watch).
//
// Every subcommand honors the root `--json` flag (text for humans,
// JSON for agents and CI automation) and the persistent `--app-url`
// flag to override app discovery.
//
// Design note: none of these commands touch the scaffolded project's
// source code. They talk to the app over HTTP the same way the
// dashboard does, so the tooling stays orthogonal to the developer's
// workflow. When `devtools` isn't set the commands fail fast with
// DEBUG_DEVTOOLS_OFF rather than hang or return misleading empty
// responses.
var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Inspect a running gofasta app via its /debug/* endpoints",
	Long: `Query the running app's devtools surface from the CLI.

Every subcommand is a structured alternative to log grepping. Commands
honor --json for machine-parseable output, share the --app-url flag
for explicit targeting, and fail with DEBUG_APP_UNREACHABLE /
DEBUG_DEVTOOLS_OFF when the target app isn't reachable or wasn't
built with the devtools tag.

Typical usage:

  gofasta debug health                    # is the app reachable + devtools-enabled?
  gofasta debug last-slow-request         # latest request > threshold + trace + logs + SQL
  gofasta debug last-error                # latest panic with surrounding context
  gofasta debug requests --slower-than=200ms --json
  gofasta debug trace <trace-id>
  gofasta debug n-plus-one                # every detected N+1 pattern
  gofasta debug watch --trace --errors    # live NDJSON event stream`,
}

// debugAppURL is the persistent --app-url override. Empty means
// "discover from config.yaml / env".
var debugAppURL string

func init() {
	debugCmd.PersistentFlags().StringVar(&debugAppURL, "app-url", "",
		"Override the app URL (default: discovered from config.yaml / PORT env)")
	debugCmd.GroupID = groupWorkflow
	rootCmd.AddCommand(debugCmd)
}
