package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunDebugNPlusOne_DevtoolsError — unreachable app URL short-
// circuits the requireDevtools pre-check.
func TestRunDebugNPlusOne_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugNPlusOne())
}

// TestRunDebugNPlusOne_GetJSONError — /debug/sql returns 500.
func TestRunDebugNPlusOne_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/sql")
	withDebugAppURL(t, url)
	require.Error(t, runDebugNPlusOne())
}

// TestDebugNPlusOneCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugNPlusOneCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugNPlusOneCmd.RunE(debugNPlusOneCmd, nil))
}
