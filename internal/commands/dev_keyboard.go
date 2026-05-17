package commands

import (
	"io"
	"os"

	"github.com/gofastadev/cli/internal/cliout"
	"golang.org/x/term"
)

// ─────────────────────────────────────────────────────────────────────
// Interactive keyboard control for `gofasta dev`.
//
// One byte at a time, read from stdin in raw mode. Maps single
// keystrokes to pipeline-level signals (restart / quit) so a developer
// can drive the dev loop without leaving the process. Mirrors the UX
// established by Vite / Next.js dev servers — `r` to restart, `q` to
// quit, `h` or `?` to print the keybinding help.
//
// Key design choices:
//
//   - Skip silently on non-TTY stdin (CI, piped input). The pipeline
//     still works; only the interactive layer is disabled.
//   - Skip silently when --no-keyboard is set, for the same reason
//     plus for users who pipe stdin into a child process.
//   - The listener lives for the lifetime of `runDev`, persisting
//     across restart iterations so the user can press R again
//     immediately after a restart completes.
//   - Raw-mode setup is reversed via the returned cancel function. We
//     also defer-restore on process termination so a panicked exit
//     does not leave the user's terminal in raw mode.
// ─────────────────────────────────────────────────────────────────────

// keyboardSignal is the message the listener fans into the pipeline.
type keyboardSignal int

const (
	// sigKeyboardRestart asks the pipeline to tear down and re-run
	// from scratch, equivalent to the developer typing Ctrl+C and
	// `gofasta dev` again.
	sigKeyboardRestart keyboardSignal = iota

	// sigKeyboardQuit asks the pipeline to exit cleanly. Equivalent
	// to the developer typing Ctrl+C.
	sigKeyboardQuit
)

// termIsTerminalFn / termSetCbreakFn / termRestoreFn /
// newCancelableStdinReaderFn are package-level seams over the
// corresponding x/term and self-pipe helpers so tests can stub them
// without needing a real PTY. Production assigns the real functions.
//
// termSetCbreakFn deliberately points at makeCbreak rather than
// term.MakeRaw. MakeRaw clears OPOST, which disables newline →
// carriage-return-newline translation; subprocesses like Air and the
// project's own logger then produce "staircase" output where each
// successive line starts at the column the previous line ended at.
// makeCbreak only clears ICANON+ECHO (so single-key shortcuts still
// work) and leaves OPOST/ISIG/etc. alone.
var (
	termIsTerminalFn           = term.IsTerminal
	termSetCbreakFn            = makeCbreak
	termRestoreFn              = term.Restore
	newCancelableStdinReaderFn = newCancelableStdinReader
)

// keyboardReader is the abstraction over stdin used by the listener.
// `*os.File` satisfies it; tests pass a bytes.Reader-backed fake.
type keyboardReader interface {
	Read(p []byte) (int, error)
	Fd() uintptr
}

// startKeyboardListener puts stdin in raw mode and spawns a goroutine
// that translates keystrokes into keyboardSignal values on the
// returned channel. When stdin is not a terminal (or --no-keyboard is
// set), it returns a NIL channel + a no-op cancel + active=false.
//
// The nil-channel sentinel is deliberate: a nil channel never fires in
// a `select`, so callers can put `case sig := <-keySignals:` next to
// `case <-sigChan:` and have the SIGINT path keep working without
// firing a phantom restart from a closed channel's zero value.
//
// The cancel function restores the terminal to its prior state. It is
// idempotent and safe to call from a defer.
func startKeyboardListener(in keyboardReader, disabled bool) (signals <-chan keyboardSignal, cancel func(), active bool) {
	if disabled {
		return nil, func() {}, false
	}
	fd := int(in.Fd())
	if !termIsTerminalFn(fd) {
		return nil, func() {}, false
	}
	oldState, err := termSetCbreakFn(fd)
	if err != nil {
		// Raw-mode failure is non-fatal — drop into the no-listener
		// path so the rest of the pipeline still runs.
		return nil, func() {}, false
	}

	// Wrap stdin in a self-pipe + poll(2) reader so the listener's
	// blocked Read can be canceled cleanly. On macOS, dup(2)+close
	// does NOT interrupt a pending read syscall — the kernel keeps
	// the read waiting until input arrives, at which point the
	// dying listener still eats that byte and steals it from the
	// next menu prompt. poll(2) IS reliably interrupted by POLLHUP
	// on the self-pipe's read end, which fires the instant cancel()
	// closes the write end.
	//
	// Failure to set up the self-pipe is non-fatal: restore the
	// terminal and drop into the no-listener path. Cancellation is
	// what makes the rest of the design correct, so without it we
	// choose "no shortcut" over "a shortcut that leaks goroutines
	// and steals bytes from the menu on the next iteration".
	stdinReader, err := newCancelableStdinReaderFn(fd)
	if err != nil {
		_ = termRestoreFn(fd, oldState)
		return nil, func() {}, false
	}

	sigCh := make(chan keyboardSignal, 4)
	// done is closed by cancel() so readKeyboardLoop can distinguish
	// "Read returned because the user pressed a key" from "Read
	// returned because the self-pipe was closed" and exit silently
	// in the latter case (no stray signal to a stale sigCh listener).
	done := make(chan struct{})
	go readKeyboardLoop(stdinReader, sigCh, done)

	printKeyboardBanner()

	var restored bool
	cancel = func() {
		if restored {
			return
		}
		restored = true
		close(done)
		// Closing the self-pipe's write end fires POLLHUP on the
		// read end inside the goroutine's Poll call — the Read
		// returns io.EOF, the goroutine sees `done` closed and
		// returns without consuming a keystroke from stdin.
		_ = stdinReader.Close()
		_ = termRestoreFn(fd, oldState)
	}
	return sigCh, cancel, true
}

// readKeyboardLoop reads single bytes from `in` and translates each to
// a keyboardSignal. Exits when:
//   - cancel() closes the self-pipe — `in.Read` returns io.EOF
//     (`done` is also closed, used to suppress the stray signal that
//     would otherwise be sent if a real keystroke and a cancel raced),
//     OR
//   - stdin returns EOF / error from any other cause
//
// makeCbreak sets the TTY to VMIN=1 VTIME=0, so a naive Read would
// block until at least one byte is available — and a goroutine
// parked in such a Read can't notice when the pipeline closed `done`.
// Cancellation is therefore handled inside `in`'s Read: it uses
// poll(2) on stdin AND on a self-pipe's read end, so closing the
// self-pipe's write end wakes the Poll immediately with POLLHUP and
// the goroutine exits without having consumed any keystroke. That
// leaves the next reader of os.Stdin (typically the preflight menu
// on iteration 2 of the restart loop) able to see every byte the
// user types.
//
// Unrecognized keys are silently ignored; `h` / `?` print the help
// banner inline so the user can re-discover the keybindings without
// leaving the process.
func readKeyboardLoop(in io.Reader, sigCh chan<- keyboardSignal, done <-chan struct{}) {
	buf := make([]byte, 1)
	for {
		select {
		case <-done:
			return
		default:
		}
		n, err := in.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		// Re-check done after Read so that a cancellation that landed
		// while we were briefly blocked is honored before the byte is
		// interpreted as r/q/h. Without this, the goroutine could send
		// one stale signal after cancel() returned.
		select {
		case <-done:
			return
		default:
		}
		switch buf[0] {
		case 'r', 'R':
			sigCh <- sigKeyboardRestart
		case 'q', 'Q':
			sigCh <- sigKeyboardQuit
		case 0x03: // Ctrl+C — same intent as Q, also forwarded as a real signal by the OS.
			sigCh <- sigKeyboardQuit
		case 'h', 'H', '?':
			printKeyboardHelp()
		}
	}
}

// printKeyboardBanner is the one-line "interactive controls active"
// notice printed after raw mode is engaged. Kept short so it does not
// add visual weight to a typical dev run.
func printKeyboardBanner() {
	cliout.Step("⌨  press `r` to restart · `q` to quit · `h` for help")
}

// printKeyboardHelp prints the full keybinding list to stdout. Called
// when the user presses `h` or `?` during the dev loop.
func printKeyboardHelp() {
	cliout.Blank()
	cliout.Step("interactive controls:")
	cliout.Plainln("   r, R    restart the dev pipeline from scratch")
	cliout.Plainln("   q, Q    quit gofasta dev (same as Ctrl+C)")
	cliout.Plainln("   h, H, ? show this help")
	cliout.Blank()
}

// _ ensures the os.File interface check happens at compile time. If
// *os.File ever stops satisfying keyboardReader (it won't, but cheap
// insurance), the build breaks here rather than at the call site.
var _ keyboardReader = (*os.File)(nil)
