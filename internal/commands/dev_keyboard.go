package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/gofastadev/cli/internal/termcolor"
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

// termIsTerminalFn / termSetCbreakFn / termRestoreFn are package-level
// seams over the corresponding x/term helpers so tests can stub them
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
	termIsTerminalFn = term.IsTerminal
	termSetCbreakFn  = makeCbreak
	termRestoreFn    = term.Restore
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

	sigCh := make(chan keyboardSignal, 4)
	go readKeyboardLoop(in, sigCh)

	printKeyboardBanner()

	var restored bool
	cancel = func() {
		if restored {
			return
		}
		restored = true
		_ = termRestoreFn(fd, oldState)
	}
	return sigCh, cancel, true
}

// readKeyboardLoop reads single bytes from `in` and translates each to
// a keyboardSignal. Exits when stdin returns EOF / error — typically
// when the cancel function restores the terminal mid-read, the next
// read errors out and the goroutine exits cleanly.
//
// Unrecognized keys are silently ignored; `h` / `?` print the help
// banner inline so the user can re-discover the keybindings without
// leaving the process.
func readKeyboardLoop(in io.Reader, sigCh chan<- keyboardSignal) {
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if err != nil || n == 0 {
			return
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
	termcolor.PrintStep("⌨  press `r` to restart · `q` to quit · `h` for help")
}

// printKeyboardHelp prints the full keybinding list to stdout. Called
// when the user presses `h` or `?` during the dev loop.
func printKeyboardHelp() {
	fmt.Println()
	termcolor.PrintStep("interactive controls:")
	fmt.Println("   r, R    restart the dev pipeline from scratch")
	fmt.Println("   q, Q    quit gofasta dev (same as Ctrl+C)")
	fmt.Println("   h, H, ? show this help")
	fmt.Println()
}

// _ ensures the os.File interface check happens at compile time. If
// *os.File ever stops satisfying keyboardReader (it won't, but cheap
// insurance), the build breaks here rather than at the call site.
var _ keyboardReader = (*os.File)(nil)
