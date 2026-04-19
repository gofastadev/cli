package commands

import (
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
