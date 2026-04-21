package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunDebugExplain_DevtoolsError — unreachable app URL short-circuits
// the requireDevtools pre-check before EXPLAIN is issued.
func TestRunDebugExplain_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugExplain("SELECT 1"))
}

// TestDebugExplainCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugExplainCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugExplainCmd.RunE(debugExplainCmd, []string{"SELECT 1"}))
}
