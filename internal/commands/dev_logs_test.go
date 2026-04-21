package commands

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestLogStreamerCancel_NilProcess — cmd.Process starts as nil before
// Start; the cancel helper handles that without error.
func TestLogStreamerCancel_NilProcess(t *testing.T) {
	cmd := exec.Command("true")
	// cmd.Process is nil until Start/Run.
	err := logStreamerCancel(cmd)
	assert.NoError(t, err)
}

// TestLogStreamerCancel_WithProcess — a running process gets killed
// and the helper returns any error from Kill (typically nil).
func TestLogStreamerCancel_WithProcess(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() })
	_ = logStreamerCancel(cmd) // may return a benign error if process already dying
	// Wait for the process to actually exit.
	_ = cmd.Wait()
}

// TestLogStreamerWatch_NilProcess — ctx cancellation on a never-
// started command just returns without panicking.
func TestLogStreamerWatch_NilProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command("true")
	cancel()
	logStreamerWatch(ctx, cmd)
}

// TestMakeLogStreamerCancel — exercises the closure body.
func TestMakeLogStreamerCancel(t *testing.T) {
	cmd := exec.Command("true")
	fn := makeLogStreamerCancel(cmd)
	// Before Start, cmd.Process is nil → nil.
	assert.NoError(t, fn())
}

// TestLogStreamerWatch_WithProcess — ctx cancellation while process
// is running triggers the Kill branch.
func TestLogStreamerWatch_WithProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	done := make(chan struct{})
	go func() {
		logStreamerWatch(ctx, cmd)
		close(done)
	}()
	cancel()
	<-done
	_ = cmd.Wait()
}
