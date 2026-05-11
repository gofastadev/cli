//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package commands

import (
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// makeCbreak places fd in cbreak mode for the duration of the dev
// session: single-byte reads (VMIN=1, VTIME=0), no echo, no line
// buffering. Everything else — crucially OPOST (NL → CRLF on output)
// and ISIG (Ctrl+C → SIGINT) — is left intact.
//
// We deliberately do NOT call term.MakeRaw here. MakeRaw goes further
// and clears OPOST as well, which means any newline emitted to the TTY
// by a subprocess (Air, the project's logger, a Go panic stack trace)
// only moves the cursor DOWN — it does not also return to column 0.
// That produces the "staircase" output the user reported when running
// `gofasta dev`: every subsequent line indents by however many columns
// the previous line ended at. Cbreak avoids that entirely while still
// giving us single-key shortcuts for r/q/h.
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
