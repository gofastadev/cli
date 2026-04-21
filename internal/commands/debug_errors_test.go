package commands

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunDebugErrors_DevtoolsError — unreachable app URL short-circuits
// the requireDevtools pre-check.
func TestRunDebugErrors_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugErrors())
}

// TestRunDebugErrors_GetJSONError — /debug/errors returns 500.
func TestRunDebugErrors_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/errors")
	withDebugAppURL(t, url)
	require.Error(t, runDebugErrors())
}

// TestRunDebugErrors_Limit_And_MultiEntry — limit trims to N and the
// multi-entry loop body runs when limit is 0 (no trim).
func TestRunDebugErrors_Limit_And_MultiEntry(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{
				{Recovered: "boom 1"},
				{Recovered: "boom 2"},
				{Recovered: "boom 3"},
			})
		},
	})
	withDebugAppURL(t, url)
	debugErrorsContains = ""
	debugErrorsLimit = 1
	t.Cleanup(func() { debugErrorsLimit = 0 })
	require.NoError(t, runDebugErrors())
	// Reset then run with limit 0 to cover the "multi entry loop".
	debugErrorsLimit = 0
	require.NoError(t, runDebugErrors())
}

// TestDebugErrorsCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugErrorsCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugErrorsCmd.RunE(debugErrorsCmd, nil))
}
