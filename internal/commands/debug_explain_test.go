package commands

import (
	"net/http"
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

func TestRunDebugExplain_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, explainResponse{Plan: "Seq Scan on users"})
		},
	})
	withDebugAppURL(t, url)
	debugExplainVars = []string{"42"}
	t.Cleanup(func() { debugExplainVars = nil })
	require.NoError(t, runDebugExplain("SELECT * FROM users WHERE id = ?"))
}

func TestRunDebugExplain_RejectsNonSelect(t *testing.T) {
	url := debugFixture(t, nil)
	withDebugAppURL(t, url)
	err := runDebugExplain("UPDATE users SET x = 1")
	require.Error(t, err)
}

func TestRunDebugExplain_AppRejects(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		},
	})
	withDebugAppURL(t, url)
	require.Error(t, runDebugExplain("SELECT 1"))
}
