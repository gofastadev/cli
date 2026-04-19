package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for dev_logs.go:startLogStreamer.
//
// The function spawns `docker compose logs -f` in a background
// goroutine. Tests don't actually want to run docker — we stub exec
// and just verify the lifecycle (empty services short-circuits; a
// real streamer returns a cancel func that stops cleanly).
// ─────────────────────────────────────────────────────────────────────

// TestStartLogStreamer_EmptyServices — no services → no subprocess,
// cancel is a no-op func but not nil.
func TestStartLogStreamer_EmptyServices(t *testing.T) {
	cancel := startLogStreamer(nil)
	assert.NotNil(t, cancel)
	cancel() // should not panic
	cancel = startLogStreamer([]string{})
	assert.NotNil(t, cancel)
	cancel()
}

// TestStartLogStreamer_WithServices — with a stubbed exec, the
// streamer launches a fake subprocess and returns a live cancel
// func. Calling cancel() tears it down cleanly.
func TestStartLogStreamer_WithServices(t *testing.T) {
	// Stub execCommand with a quick-exiting fake so the streamer's
	// background goroutines don't leak a real docker process.
	withFakeExec(t, 0)
	cancel := startLogStreamer([]string{"db", "cache"})
	assert.NotNil(t, cancel)
	cancel() // should not panic
}
