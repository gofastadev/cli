package commands

import (
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunDebugHar_DevtoolsError — unreachable app URL short-circuits
// the requireDevtools pre-check.
func TestRunDebugHar_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	debugHarOutput = ""
	require.Error(t, runDebugHar())
}

// TestRunDebugHar_GetJSONError — /debug/requests returns 500.
func TestRunDebugHar_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/requests")
	withDebugAppURL(t, url)
	debugHarOutput = ""
	require.Error(t, runDebugHar())
}

// TestRunDebugHar_EncodeFails — harOutOverride points at an errWriter
// so json.NewEncoder.Encode fails.
func TestRunDebugHar_EncodeFails(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
	})
	withDebugAppURL(t, url)
	debugHarOutput = ""
	harOutOverride = errWriter{}
	t.Cleanup(func() { harOutOverride = nil })
	err := runDebugHar()
	require.Error(t, err)
}

// TestRunDebugHar_CreateFails — point debugHarOutput at a path under a
// nonexistent directory so os.Create fails.
func TestRunDebugHar_CreateFails(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
	})
	withDebugAppURL(t, url)
	debugHarOutput = "/nonexistent-dir/subdir/file.har"
	t.Cleanup(func() { debugHarOutput = "" })
	require.Error(t, runDebugHar())
}

// TestRunDebugHar_EncodeError — documented: /dev/full only exists on
// Linux. On systems where it isn't present we skip; where it is, a
// write to it makes the encoder fail.
func TestRunDebugHar_EncodeError(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full not available on this OS")
	}
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
	})
	withDebugAppURL(t, url)
	debugHarOutput = "/dev/full"
	t.Cleanup(func() { debugHarOutput = "" })
	require.Error(t, runDebugHar())
}

// TestRunDebugHar_EncodeFailure — json.NewEncoder.Encode of a HAR
// struct cannot fail without a Writer seam; the seam-based case is
// TestRunDebugHar_EncodeFails above.
func TestRunDebugHar_EncodeFailure(t *testing.T) {
	t.Skip("json.NewEncoder.Encode of HAR struct cannot fail; would need io.Writer seam")
}

// TestDebugHarCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugHarCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugHarCmd.RunE(debugHarCmd, nil))
}
