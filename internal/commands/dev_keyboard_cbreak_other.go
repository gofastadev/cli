//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly

package commands

import (
	"errors"

	"golang.org/x/term"
)

// makeCbreak fallback for platforms without a POSIX termios interface
// available through golang.org/x/sys/unix (Windows, JS, Plan 9). On
// these platforms we fall back to term.MakeRaw — Windows in particular
// uses console mode flags rather than termios, and term.MakeRaw is
// already the documented way to get single-key reads there.
//
// The OPOST-equivalent line-ending translation on Windows is handled
// by the terminal driver, not by a termios bit, so MakeRaw does NOT
// produce the staircase effect there — the fallback is safe.
func makeCbreak(fd int) (*term.State, error) {
	return term.MakeRaw(fd)
}

// cancelableStdinReader and newCancelableStdinReader are unavailable
// on non-POSIX platforms (no portable poll(2) + self-pipe pair).
// Returning a no-op stub plus error makes startKeyboardListener
// degrade to "no listener" rather than carry the goroutine-leak bug
// on platforms we don't actively support.
type cancelableStdinReader struct{}

func (cancelableStdinReader) Read(_ []byte) (int, error) { return 0, errors.New("not supported") }
func (cancelableStdinReader) Close() error               { return nil }
func (cancelableStdinReader) Fd() uintptr                { return 0 }

func newCancelableStdinReader(_ int) (*cancelableStdinReader, error) {
	return nil, errors.New("self-pipe cancellation not supported on this platform")
}
