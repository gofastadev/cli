package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for dev_keyboard.go.
//
// startKeyboardListener depends on x/term primitives that need a real
// PTY — fine in production, hostile to `go test`. We stub via the
// package-level seams (termIsTerminalFn / termSetCbreakFn / termRestoreFn)
// so the listener's branching logic can be exercised without a TTY.
// ─────────────────────────────────────────────────────────────────────

// fakeKeyboardReader is the in-memory stdin used by the listener tests.
// It satisfies keyboardReader by wrapping a bytes.Reader plus a fake fd.
type fakeKeyboardReader struct {
	*bytes.Reader
	fd uintptr
}

func (f *fakeKeyboardReader) Fd() uintptr { return f.fd }

func newFakeKB(input string) *fakeKeyboardReader {
	return &fakeKeyboardReader{Reader: bytes.NewReader([]byte(input)), fd: 99}
}

// withTerminalStubs swaps in fakes for all three x/term seams for the
// duration of the test. Each callback is called with the values
// startKeyboardListener passed.
func withTerminalStubs(t *testing.T, isTTY bool, makeRawErr error) {
	t.Helper()
	origIs, origMake, origRestore := termIsTerminalFn, termSetCbreakFn, termRestoreFn
	t.Cleanup(func() {
		termIsTerminalFn = origIs
		termSetCbreakFn = origMake
		termRestoreFn = origRestore
	})
	termIsTerminalFn = func(_ int) bool { return isTTY }
	termSetCbreakFn = func(_ int) (*term.State, error) {
		if makeRawErr != nil {
			return nil, makeRawErr
		}
		return &term.State{}, nil
	}
	termRestoreFn = func(_ int, _ *term.State) error { return nil }
}

// TestStartKeyboardListener_NotATerminal — the no-TTY path returns a
// nil channel + no-op cancel + active=false, so non-interactive sessions
// (CI, piped stdin) get the same pipeline as before this feature.
func TestStartKeyboardListener_NotATerminal(t *testing.T) {
	withTerminalStubs(t, false, nil)
	signals, cancel, active := startKeyboardListener(newFakeKB(""), false)
	assert.Nil(t, signals, "non-TTY must return a nil channel so select{} blocks correctly")
	assert.False(t, active)
	cancel() // must not panic
}

// TestStartKeyboardListener_Disabled — the --no-keyboard opt-out
// short-circuits before the TTY check, so the listener doesn't even
// query stdin.
func TestStartKeyboardListener_Disabled(t *testing.T) {
	// IsTerminal would return true if asked; the disabled flag wins.
	withTerminalStubs(t, true, nil)
	signals, _, active := startKeyboardListener(newFakeKB("r"), true)
	assert.Nil(t, signals)
	assert.False(t, active)
}

// TestStartKeyboardListener_MakeRawFails — raw-mode failure is
// non-fatal: log nothing, return the no-listener sentinel, let the
// pipeline continue without keyboard support.
func TestStartKeyboardListener_MakeRawFails(t *testing.T) {
	withTerminalStubs(t, true, errors.New("ioctl boom"))
	signals, _, active := startKeyboardListener(newFakeKB("r"), false)
	assert.Nil(t, signals)
	assert.False(t, active)
}

// TestReadKeyboardLoop_RestartKey — `r` and `R` both produce
// sigKeyboardRestart on the channel.
func TestReadKeyboardLoop_RestartKey(t *testing.T) {
	for _, in := range []string{"r", "R"} {
		ch := make(chan keyboardSignal, 4)
		readKeyboardLoop(strings.NewReader(in), ch, nil)
		require.Len(t, ch, 1, "input %q produced %d signals; want 1", in, len(ch))
		assert.Equal(t, sigKeyboardRestart, <-ch)
	}
}

// TestReadKeyboardLoop_QuitKey — `q`, `Q`, and Ctrl+C all produce
// sigKeyboardQuit. Ctrl+C is also forwarded by the OS as SIGINT, but
// having the keyboard layer recognize it means a user inside `gofasta
// dev` can quit even if they're tunneled through something that eats
// signals (rare, but the cost of supporting it is one byte).
func TestReadKeyboardLoop_QuitKey(t *testing.T) {
	for _, in := range []string{"q", "Q", "\x03"} {
		ch := make(chan keyboardSignal, 4)
		readKeyboardLoop(strings.NewReader(in), ch, nil)
		require.Len(t, ch, 1, "input %q produced %d signals; want 1", in, len(ch))
		assert.Equal(t, sigKeyboardQuit, <-ch)
	}
}

// TestReadKeyboardLoop_HelpKey — `h`, `H`, `?` print the help banner
// and emit no pipeline signal. We don't capture stdout here (the
// human-side banner uses termcolor); the contract tested is "help
// keys do not produce sigKeyboardRestart or sigKeyboardQuit".
func TestReadKeyboardLoop_HelpKey(t *testing.T) {
	for _, in := range []string{"h", "H", "?"} {
		ch := make(chan keyboardSignal, 4)
		readKeyboardLoop(strings.NewReader(in), ch, nil)
		assert.Empty(t, ch, "input %q must not emit a pipeline signal", in)
	}
}

// TestReadKeyboardLoop_UnknownKeysIgnored — random keys (a, space,
// digits) are silently dropped so a leaning keyboard or paste does not
// trigger spurious restarts.
func TestReadKeyboardLoop_UnknownKeysIgnored(t *testing.T) {
	ch := make(chan keyboardSignal, 8)
	readKeyboardLoop(strings.NewReader("abc 123\n\t"), ch, nil)
	assert.Empty(t, ch)
}

// TestReadKeyboardLoop_MixedSequence — a realistic burst: garbage,
// then R, then garbage, then Q. The loop emits exactly two signals in
// the expected order.
func TestReadKeyboardLoop_MixedSequence(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	readKeyboardLoop(strings.NewReader("xRyq"), ch, nil)
	close(ch)
	got := make([]keyboardSignal, 0, 2)
	for s := range ch {
		got = append(got, s)
	}
	assert.Equal(t, []keyboardSignal{sigKeyboardRestart, sigKeyboardQuit}, got)
}

// TestRunInDockerSupervisor_RestartSignal — when the user presses R
// (sigKeyboardRestart on the channel), the supervisor calls teardown
// with reason "restart" and returns true so the outer pipeline loop
// re-runs from scratch.
//
// Subtle: `exited` MUST NOT be ready at the moment the outer select
// fires, or Go's random-case selection can pick it over keySignals
// (50% failure rate). We deliver to it from a delayed goroutine so
// the outer select definitively picks keySignals first; the inner
// `<-exited` (after interruptCompose) then unblocks on the delivery.
func TestRunInDockerSupervisor_RestartSignal(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	keyCh <- sigKeyboardRestart
	go func() {
		time.Sleep(50 * time.Millisecond)
		exited <- nil
	}()
	var called string
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.True(t, restart)
	assert.Equal(t, "restart", called)
}

// TestRunInDockerSupervisor_QuitSignal — Q press is the same teardown
// path as Ctrl+C; restart=false so the outer loop exits.
// Same delayed-exited pattern as the Restart test (see comment above).
func TestRunInDockerSupervisor_QuitSignal(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	keyCh <- sigKeyboardQuit
	go func() {
		time.Sleep(50 * time.Millisecond)
		exited <- nil
	}()
	var called string
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.Equal(t, "quit", called)
}

// TestRunInDockerSupervisor_ChildExits — if the foreground compose
// process exits on its own (app crashed, container died), the
// supervisor must call teardown("app-exited") and return false so the
// outer loop exits cleanly. This is the path the user gets when the
// app inside the container crashes.
func TestRunInDockerSupervisor_ChildExits(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	exited <- nil // child exited cleanly
	var called string
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.False(t, restart)
	assert.Equal(t, "app-exited", called)
}

// printKeyboardBanner — invoked once to register coverage on the
// banner-print function (no other test calls it directly).
func TestPrintKeyboardBanner_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() { printKeyboardBanner() })
}

// startKeyboardListener happy path — all seams succeed, listener
// launches and returns a non-nil signals channel + active=true. The
// race detector requires cancelableStdinReader.Close to serialize with
// in-flight Read (see the readMu inside cancelableStdinReader).
func TestStartKeyboardListener_HappyPath(t *testing.T) {
	withTerminalStubs(t, true, nil)
	origNew := newCancelableStdinReaderFn
	newCancelableStdinReaderFn = func(fd int) (*cancelableStdinReader, error) {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		return &cancelableStdinReader{fd: fd, cancelR: r, cancelW: w}, nil
	}
	t.Cleanup(func() { newCancelableStdinReaderFn = origNew })

	signals, cancel, active := startKeyboardListener(newFakeKB(""), false)
	require.True(t, active)
	assert.NotNil(t, signals)
	cancel()
	cancel() // idempotent
}

// startKeyboardListener — newCancelableStdinReaderFn returns an error;
// listener drops into the no-listener path and termRestore is invoked
// to undo cbreak before returning the failure tuple.
func TestStartKeyboardListener_StdinReaderConstructionFails(t *testing.T) {
	withTerminalStubs(t, true, nil)
	origNew := newCancelableStdinReaderFn
	newCancelableStdinReaderFn = func(int) (*cancelableStdinReader, error) {
		return nil, errors.New("pipe boom")
	}
	t.Cleanup(func() { newCancelableStdinReaderFn = origNew })

	signals, cancel, active := startKeyboardListener(newFakeKB(""), false)
	assert.Nil(t, signals)
	assert.False(t, active)
	cancel() // no-op cancel must not panic
}

// startKeyboardListener no-op cancel closures — when active=false, the
// returned `cancel` is an empty `func() {}`. Existing tests check
// return values but never invoke this closure; calling it covers the
// empty function body.
func TestStartKeyboardListener_NoopCancelClosuresAreCallable(t *testing.T) {
	withTerminalStubs(t, true, errors.New("force raw failure"))
	_, cancel, active := startKeyboardListener(newFakeKB(""), false)
	assert.False(t, active)
	assert.NotPanics(t, func() { cancel() })
}

// readKeyboardLoop — `done` closed before any Read call → loop returns
// without ever touching the reader.
func TestReadKeyboardLoop_DoneClosedBeforeRead(t *testing.T) {
	ch := make(chan keyboardSignal, 1)
	done := make(chan struct{})
	close(done)
	r := &neverReadReader{}
	readKeyboardLoop(r, ch, done)
	assert.Equal(t, 0, r.calls, "Read must not be called when done is closed first")
	assert.Empty(t, ch)
}

// readKeyboardLoop — `done` closed AFTER Read returns one byte but
// BEFORE the post-Read switch interprets it. The re-check at the top
// of the next iteration suppresses the would-be signal.
func TestReadKeyboardLoop_DoneClosedAfterRead(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	done := make(chan struct{})
	r := &gateReader{
		bytes:     []byte{'r'},
		afterRead: func() { close(done) },
	}
	readKeyboardLoop(r, ch, done)
	assert.Empty(t, ch, "post-Read done check must suppress the signal")
}

// readKeyboardLoop — Read returns (0, nil) then EOF. The `if n == 0
// { continue }` branch fires.
func TestReadKeyboardLoop_ZeroByteRead(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	done := make(chan struct{})
	r := &zeroByteThenEOFReader{}
	readKeyboardLoop(r, ch, done)
	assert.Empty(t, ch)
	assert.GreaterOrEqual(t, r.calls, 2, "loop should retry after n==0")
}

// neverReadReader counts Read invocations; used to verify
// readKeyboardLoop's done-first branch never touches the reader.
type neverReadReader struct{ calls int }

func (n *neverReadReader) Read(p []byte) (int, error) {
	n.calls++
	return 0, io.EOF
}

// zeroByteThenEOFReader returns (0, nil) on first call then EOF — used
// to exercise readKeyboardLoop's `if n == 0 { continue }` branch.
type zeroByteThenEOFReader struct{ calls int }

func (z *zeroByteThenEOFReader) Read(p []byte) (int, error) {
	z.calls++
	if z.calls == 1 {
		return 0, nil
	}
	return 0, io.EOF
}

// gateReader returns one queued byte then runs afterRead, then EOFs.
type gateReader struct {
	bytes     []byte
	afterRead func()
	pos       int
}

func (g *gateReader) Read(p []byte) (int, error) {
	if g.pos >= len(g.bytes) {
		return 0, io.EOF
	}
	p[0] = g.bytes[g.pos]
	g.pos++
	if g.afterRead != nil {
		g.afterRead()
	}
	return 1, nil
}
