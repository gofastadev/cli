package commands

import (
	"context"
	"os"
	"os/exec"
)

// startLogStreamer runs `docker compose logs -f` for the selected
// services in the background and pipes its output to our stdout. The
// compose CLI already prefixes each line with the service name in a
// consistent format, so we rely on that rather than re-implementing
// prefixing ourselves.
//
// The returned cancel function stops the streamer cleanly; it's wired
// to the same teardown path as the compose services, so Ctrl+C stops
// both simultaneously.
func startLogStreamer(services []string) (cancel func()) {
	if len(services) == 0 {
		return func() {}
	}

	ctx, cancelCtx := context.WithCancel(context.Background())

	// `docker compose logs -f` attaches to live streams for the named
	// services and tails forever. Using exec.CommandContext so cancel
	// truly kills the child process on teardown instead of leaking it.
	args := append([]string{"compose", "logs", "-f"}, services...)
	cmd := execCommand("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Explicitly set Cancel so Go's os/exec contract lets us interrupt
	// the child via the returned ctx cancellation. This works even when
	// execCommand is the real exec.Command; test stubs of execCommand
	// that don't set Cancel will simply have a no-op cancel behavior.
	cmd.Cancel = makeLogStreamerCancel(cmd)

	go func() { logStreamerWatch(ctx, cmd) }()

	// Fire-and-forget: cmd.Run() blocks; we do not wait for it. If the
	// streamer exits early (e.g. compose daemon disappeared) the dev
	// command continues running — the log stream is a nice-to-have, not
	// a correctness primitive.
	go func() { _ = cmd.Run() }()

	return cancelCtx
}

// logStreamerCancel is the Cancel callback — isolated so tests can
// exercise both the "process running" and "process nil" branches
// without relying on os/exec's internal cancellation timing.
func logStreamerCancel(cmd *exec.Cmd) error {
	if cmd.Process != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// makeLogStreamerCancel is a thin constructor around logStreamerCancel.
// Used by startLogStreamer so the closure body is trivially testable.
func makeLogStreamerCancel(cmd *exec.Cmd) func() error {
	return func() error { return logStreamerCancel(cmd) }
}

// logStreamerWatch is the background goroutine body — waits for ctx
// to complete and then kills the child if it's still running.
func logStreamerWatch(ctx context.Context, cmd *exec.Cmd) {
	<-ctx.Done()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
