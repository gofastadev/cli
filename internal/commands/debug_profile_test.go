package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveProfileDuration_CPUDefault — cpu with no override gets 30s.
func TestResolveProfileDuration_CPUDefault(t *testing.T) {
	d, err := resolveProfileDuration("cpu", "")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, d)
}

// TestResolveProfileDuration_CPUOverride — custom duration parses.
func TestResolveProfileDuration_CPUOverride(t *testing.T) {
	d, err := resolveProfileDuration("cpu", "10s")
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, d)
}

// TestResolveProfileDuration_TraceDefault — trace defaults to 5s.
func TestResolveProfileDuration_TraceDefault(t *testing.T) {
	d, err := resolveProfileDuration("trace", "")
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, d)
}

// TestResolveProfileDuration_NonTimed — heap/goroutine/etc return 0.
func TestResolveProfileDuration_NonTimed(t *testing.T) {
	d, err := resolveProfileDuration("heap", "")
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), d)
}

// TestResolveProfileDuration_BadDuration — invalid input surfaces
// DEBUG_BAD_DURATION rather than accepting a zero-duration capture.
func TestResolveProfileDuration_BadDuration(t *testing.T) {
	_, err := resolveProfileDuration("cpu", "not-a-duration")
	require.Error(t, err)
}

// TestDebugProfileKinds_CoversAllSupported — the whitelist must include
// every profile Go's net/http/pprof exposes by default.
func TestDebugProfileKinds_CoversAllSupported(t *testing.T) {
	for _, kind := range []string{
		"cpu", "heap", "goroutine", "mutex",
		"block", "allocs", "threadcreate", "trace",
	} {
		assert.True(t, debugProfileKinds[kind], "missing %q", kind)
	}
}

// TestResolveProfileDuration_TraceCustom — trace kind accepts a
// custom duration override.
func TestResolveProfileDuration_TraceCustom(t *testing.T) {
	d, err := resolveProfileDuration("trace", "3s")
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, d)
}

// TestResolveProfileDuration_TraceBadDuration — malformed trace
// override surfaces DEBUG_BAD_DURATION.
func TestResolveProfileDuration_TraceBadDuration(t *testing.T) {
	_, err := resolveProfileDuration("trace", "xyz")
	require.Error(t, err)
}

// TestRunDebugProfile_CPUWithDurationFlag — --duration forwards as
// `seconds=N` query param on /debug/pprof/profile.
func TestRunDebugProfile_CPUWithDurationFlag(t *testing.T) {
	var seenQuery string
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/health" {
			_, _ = w.Write([]byte(`{"devtools":"enabled"}`))
			return
		}
		seenQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("profile-bytes"))
	}))
	defer app.Close()

	withDebugAppURL(t, app.URL)
	debugProfileDuration = "5s"
	debugProfileOutput = ""
	t.Cleanup(func() { debugProfileDuration = ""; debugProfileOutput = "" })
	require.NoError(t, runDebugProfile("cpu"))
	assert.Contains(t, seenQuery, "seconds=5")
}

// TestRunDebugProfile_Unreachable — target app not running surfaces
// a clierr.
func TestRunDebugProfile_Unreachable(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugProfile("heap"))
}

// TestRunDebugProfile_CannotOpenOutput — writing to a path under a
// nonexistent parent directory surfaces a FILE_IO error.
func TestRunDebugProfile_CannotOpenOutput(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/health" {
			_, _ = w.Write([]byte(`{"devtools":"enabled"}`))
			return
		}
		_, _ = w.Write([]byte("bytes"))
	}))
	defer app.Close()

	withDebugAppURL(t, app.URL)
	debugProfileOutput = filepath.Join(string(os.PathSeparator)+"nonexistent-parent-for-test", "out.pprof")
	t.Cleanup(func() { debugProfileOutput = "" })
	require.Error(t, runDebugProfile("heap"))
}
