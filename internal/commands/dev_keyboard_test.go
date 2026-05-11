package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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
		readKeyboardLoop(strings.NewReader(in), ch)
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
		readKeyboardLoop(strings.NewReader(in), ch)
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
		readKeyboardLoop(strings.NewReader(in), ch)
		assert.Empty(t, ch, "input %q must not emit a pipeline signal", in)
	}
}

// TestReadKeyboardLoop_UnknownKeysIgnored — random keys (a, space,
// digits) are silently dropped so a leaning keyboard or paste does not
// trigger spurious restarts.
func TestReadKeyboardLoop_UnknownKeysIgnored(t *testing.T) {
	ch := make(chan keyboardSignal, 8)
	readKeyboardLoop(strings.NewReader("abc 123\n\t"), ch)
	assert.Empty(t, ch)
}

// TestReadKeyboardLoop_MixedSequence — a realistic burst: garbage,
// then R, then garbage, then Q. The loop emits exactly two signals in
// the expected order.
func TestReadKeyboardLoop_MixedSequence(t *testing.T) {
	ch := make(chan keyboardSignal, 4)
	readKeyboardLoop(strings.NewReader("xRyq"), ch)
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
func TestRunInDockerSupervisor_RestartSignal(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	var called string
	keyCh <- sigKeyboardRestart
	// The fake `exited` channel must complete after the supervisor
	// signals the compose process; we close it immediately so the
	// internal `<-exited` blocking call returns without a real child.
	close(exited)
	restart := runInDockerSupervisor(nil, exited, func(r string) { called = r }, keyCh)
	assert.True(t, restart)
	assert.Equal(t, "restart", called)
}

// TestRunInDockerSupervisor_QuitSignal — Q press is the same teardown
// path as Ctrl+C; restart=false so the outer loop exits.
func TestRunInDockerSupervisor_QuitSignal(t *testing.T) {
	keyCh := make(chan keyboardSignal, 1)
	exited := make(chan error, 1)
	var called string
	keyCh <- sigKeyboardQuit
	close(exited)
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
