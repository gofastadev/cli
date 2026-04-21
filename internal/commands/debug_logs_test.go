package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunDebugLogs_DevtoolsError — unreachable app URL short-circuits
// the requireDevtools pre-check.
func TestRunDebugLogs_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugLogs())
}

// TestRunDebugLogs_GetJSONError — /debug/logs returns 500.
func TestRunDebugLogs_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/logs")
	withDebugAppURL(t, url)
	require.Error(t, runDebugLogs())
}

// TestDebugLogsCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugLogsCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugLogsCmd.RunE(debugLogsCmd, nil))
}
