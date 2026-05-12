//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package commands

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// makeCbreak places fd in cbreak mode for the duration of the dev
// session: blocking reads (VMIN=1, VTIME=0 — return as soon as ≥1
// byte is available), no echo, no line buffering. Everything else —
// crucially OPOST (NL → CRLF on output) and ISIG (Ctrl+C → SIGINT) —
// is left intact.
//
// Why blocking (VMIN=1 VTIME=0) and NOT polling (VMIN=0 VTIME=1):
// Go's stdlib treats `*os.File`-wrapped TTY fds as files with
// `ZeroReadIsEOF=true`. With VMIN=0 VTIME=1 the kernel returns
// `n=0, err=nil` whenever the 100ms quantum elapses without input;
// internal/poll then maps that to `io.EOF` and the listener
// goroutine exits within ~100ms of startup, BEFORE the user ever
// presses a key. That regressed r/q/h entirely.
//
// Blocking reads bring the original cancellation problem back: a
// goroutine parked in Read can't notice when the pipeline closed
// `done`, and the next keystroke after cancel gets eaten by the
// dying listener — stealing input from the preflight menu on
// iteration 2 of the restart loop. We fix that without giving up
// blocking reads by routing the listener's input through
// cancelableStdinReader: it uses poll(2) on stdin AND on a self-
// pipe, so closing the self-pipe's write end wakes the Poll
// immediately via POLLHUP, the goroutine exits cleanly, and the
// next reader of os.Stdin sees every byte the user types.
//
// We deliberately do NOT call term.MakeRaw here. MakeRaw goes further
// and clears OPOST as well, which produces "staircase" output when
// subprocesses like Air emit newlines.
//
// The returned *term.State captures the *prior* termios via
// term.GetState, so term.Restore puts everything back the way the user
// had it — including any non-default flags they had set in their
// terminal emulator.
func makeCbreak(fd int) (*term.State, error) {
	oldState, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermiosReq)
	if err != nil {
		return nil, err
	}
	t.Lflag &^= unix.ICANON | unix.ECHO
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlSetTermiosReq, t); err != nil {
		return nil, err
	}
	return oldState, nil
}

// cancelableStdinReader is an io.Reader-backed cancellable view of
// stdin built on the self-pipe + poll(2) pattern. The listener
// goroutine reads from it; cancel() closes the self-pipe's write
// end, which wakes the in-flight Poll on the read end via POLLHUP
// — even when the goroutine is "blocked" waiting for input. Read
// then returns io.EOF and the goroutine exits without having
// consumed any byte from stdin.
//
// Why not dup(2) + close: on macOS the kernel does NOT abort a
// pending read(2) when another thread closes the fd. The closed
// dup just sits there, the read keeps waiting, and the next
// keystroke is still eaten by the dying listener — re-introducing
// the exact bug the cancellation is meant to fix. poll(2) IS
// reliably interrupted by POLLHUP on Darwin/Linux/BSD, which is
// why this implementation uses Poll as the cancellation point
// rather than a blocking Read on a dup.
type cancelableStdinReader struct {
	fd               int      // stdin fd to read from (NOT owned, never closed)
	cancelR, cancelW *os.File // self-pipe: closing cancelW wakes Poll on cancelR via POLLHUP
}

// Read blocks (via Poll) until either:
//   - stdin has a byte available — read it into p, return (1, nil)
//   - cancel() has been called — return (0, io.EOF)
//   - poll(2) errors out for an unrecoverable reason — return (0, err)
//
// The buffer is read one byte at a time. Calls with len(p) == 0 are
// treated as no-ops to match io.Reader's contract.
func (c *cancelableStdinReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	fds := []unix.PollFd{
		{Fd: int32(c.fd), Events: unix.POLLIN},
		{Fd: int32(c.cancelR.Fd()), Events: unix.POLLIN},
	}
	for {
		_, err := unix.Poll(fds, -1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		// Cancellation: write-end closed, read-end sees POLLHUP
		// (or POLLIN if a byte was queued, which we never do).
		if fds[1].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return 0, io.EOF
		}
		// Stdin: a byte is ready. Read exactly one — Poll said it's
		// available, so this won't block. Match the existing
		// readKeyboardLoop contract of one byte per Read.
		if fds[0].Revents&unix.POLLIN != 0 {
			buf := make([]byte, 1)
			n, err := unix.Read(c.fd, buf)
			if err != nil {
				if err == unix.EINTR {
					continue
				}
				return 0, err
			}
			if n == 0 {
				// EOF on stdin (rare on a TTY — happens if the
				// controlling terminal goes away). Tell the listener.
				return 0, io.EOF
			}
			p[0] = buf[0]
			return 1, nil
		}
		// Spurious wakeup (POLLERR/POLLNVAL on stdin, etc.). Treat as
		// an error so the listener stops instead of busy-looping.
		if fds[0].Revents != 0 {
			return 0, io.EOF
		}
	}
}

// Close releases the self-pipe. Safe to call multiple times.
func (c *cancelableStdinReader) Close() error {
	if c.cancelW != nil {
		_ = c.cancelW.Close()
		c.cancelW = nil
	}
	if c.cancelR != nil {
		_ = c.cancelR.Close()
		c.cancelR = nil
	}
	return nil
}

// Fd reports the underlying stdin fd. The listener never reads
// directly through this — it goes through Read above so cancellation
// works — but startKeyboardListener's bookkeeping wants the fd for
// termios restore, and tests use it for assertion purposes.
func (c *cancelableStdinReader) Fd() uintptr { return uintptr(c.fd) }

// newCancelableStdinReader builds a cancellation-aware reader around
// stdin's fd. Returns an io.ReadCloser-like value plus a separate
// cancel function (Close on the value is what cancel calls under the
// hood). Failure to set up the self-pipe is non-fatal: the caller
// degrades to "no listener" rather than carry the goroutine-leak bug.
//
// Sets CLOEXEC on both pipe fds so child processes (Air, docker
// compose) don't inherit them.
func newCancelableStdinReader(fd int) (*cancelableStdinReader, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// os.Pipe sets O_CLOEXEC on POSIX platforms by default in
	// modern Go versions; setting it explicitly here is belt-and-
	// braces for older Go builds and clearer intent for readers.
	if rawR, err := r.SyscallConn(); err == nil {
		_ = rawR.Control(func(fd uintptr) { unix.CloseOnExec(int(fd)) })
	}
	if rawW, err := w.SyscallConn(); err == nil {
		_ = rawW.Control(func(fd uintptr) { unix.CloseOnExec(int(fd)) })
	}
	return &cancelableStdinReader{fd: fd, cancelR: r, cancelW: w}, nil
}
