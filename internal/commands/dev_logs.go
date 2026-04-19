package commands

import (
	"context"
	"os"
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
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}

	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	// Fire-and-forget: cmd.Run() blocks; we do not wait for it. If the
	// streamer exits early (e.g. compose daemon disappeared) the dev
	// command continues running — the log stream is a nice-to-have, not
	// a correctness primitive.
	go func() { _ = cmd.Run() }()

	return cancelCtx
}
