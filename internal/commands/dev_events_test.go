package commands

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// dev_events covers every emitter method on both jsonEmitter and
// humanEmitter. JSON variants round-trip through encoding/json; human
// variants are called for their side-effects on stdout (we just
// verify they don't panic and produce output).
// ─────────────────────────────────────────────────────────────────────

// TestJSONEmitter_AllEvents — cycles through every emitter method,
// asserts the emitted line parses as JSON with the right `event` +
// status fields.
func TestJSONEmitter_AllEvents(t *testing.T) {
	var buf bytes.Buffer
	e := &jsonEmitter{out: &buf}

	e.Preflight("28.0", "v2.26")
	e.ServiceStart("db")
	e.ServiceHealthy("db", 250*time.Millisecond)
	e.ServiceUnhealthy("cache", "timeout")
	e.MigrateOK(3)
	e.MigrateSkipped("--no-migrate")
	e.Air(8080, map[string]string{"rest": "http://localhost:8080"})
	e.Shutdown("stopped", 0)
	e.Info("air starting")
	e.Warn("dashboard died")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
	require.Len(t, lines, 10)

	expected := []struct {
		event  string
		status string
	}{
		{"preflight", "ok"},
		{"service", "starting"},
		{"service", "healthy"},
		{"service", "unhealthy"},
		{"migrate", "ok"},
		{"migrate", "skipped"},
		{"air", "running"},
		{"shutdown", ""},
		{"info", ""},
		{"warn", ""},
	}
	for i, line := range lines {
		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(line, &got), "line=%s", line)
		assert.Equal(t, expected[i].event, got["event"])
		if expected[i].status != "" {
			assert.Equal(t, expected[i].status, got["status"])
		}
	}
}

// TestJSONEmitter_ServiceHealthy_DurationMS — the elapsed time is
// serialized in milliseconds so agents can compare service startup
// times without parsing Go's duration format.
func TestJSONEmitter_ServiceHealthy_DurationMS(t *testing.T) {
	var buf bytes.Buffer
	e := &jsonEmitter{out: &buf}
	e.ServiceHealthy("db", 1500*time.Millisecond)
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got))
	assert.Equal(t, float64(1500), got["duration_ms"])
}

// TestJSONEmitter_AirURLs — URLs round-trip as a nested object.
func TestJSONEmitter_AirURLs(t *testing.T) {
	var buf bytes.Buffer
	e := &jsonEmitter{out: &buf}
	e.Air(8080, map[string]string{"rest": "http://x", "swagger": "http://x/swagger"})
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got))
	urls, ok := got["urls"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "http://x", urls["rest"])
}

// TestHumanEmitter_DoesNotPanic — every method runs without panicking
// under a TTY-less test environment. We don't assert on stdout
// content because the output is intentionally human-formatted (colors,
// emoji); just confirming the switch cases don't explode.
func TestHumanEmitter_DoesNotPanic(t *testing.T) {
	h := &humanEmitter{}
	h.Preflight("28.0", "v2.26")
	h.ServiceStart("db")
	h.ServiceHealthy("db", 200*time.Millisecond)
	h.ServiceUnhealthy("cache", "timeout")
	h.MigrateOK(3)
	h.MigrateOK(0) // zero-applied branch — "up to date"
	h.MigrateSkipped("flag")
	h.Air(8080, map[string]string{"rest": "http://localhost:8080"})
	h.Shutdown("stopped", 0)
	h.Info("message")
	h.Warn("warning")
}

// TestNewDevEmitter_JSON — when jsonMode is on, newDevEmitter returns
// a jsonEmitter; otherwise a humanEmitter.
func TestNewDevEmitter_JSONMode(t *testing.T) {
	// Swap out the cliout mode for the duration of the test.
	e := newDevEmitter(true)
	_, ok := e.(*jsonEmitter)
	assert.True(t, ok, "expected jsonEmitter when json=true")

	e = newDevEmitter(false)
	_, ok = e.(*humanEmitter)
	assert.True(t, ok, "expected humanEmitter when json=false")
}
