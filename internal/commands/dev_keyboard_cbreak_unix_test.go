//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package commands

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Tier 4 coverage — the unix-only cbreak/poll code in
// dev_keyboard_cbreak_unix.go. Three functions require a real PTY pair:
// makeCbreak (sets termios on a TTY fd), cancelableStdinReader.Read
// (uses unix.Poll on stdin), and newCancelableStdinReader (allocates a
// self-pipe). The accessors Close/Fd just need an instance.
//
// We allocate the PTY via github.com/creack/pty, which uses /dev/ptmx
// under the hood and works inside CI containers without a host TTY.

// makeCbreak happy path — apply cbreak mode to a real PTY slave fd.
// Restore the prior termios via term.Restore so the deferred cleanup
// doesn't strand the PTY in an odd state for the next test.
func TestMakeCbreak_AppliesAndReturnsPriorState(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	oldState, err := makeCbreak(int(slave.Fd()))
	require.NoError(t, err)
	require.NotNil(t, oldState, "GetState should return the prior termios snapshot")

	// Restore — must succeed since oldState came from this same fd.
	require.NoError(t, term.Restore(int(slave.Fd()), oldState))
}

// makeCbreak error path — pass a non-TTY fd (a regular pipe). term.GetState
// fails with ENOTTY which makeCbreak surfaces as an error.
func TestMakeCbreak_NonTTYError(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	_, err = makeCbreak(int(r.Fd()))
	assert.Error(t, err, "non-TTY fd should fail termios ioctl")
}

// newCancelableStdinReader happy path — allocates a self-pipe pair and
// returns a non-nil reader pointing at the given fd. The cancel pipe's
// read/write ends are non-nil.
func TestNewCancelableStdinReader_HappyPath(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	require.NotNil(t, rdr)
	assert.NotNil(t, rdr.cancelR)
	assert.NotNil(t, rdr.cancelW)
	assert.Equal(t, slave.Fd(), rdr.Fd())

	// Close cleans up the self-pipe.
	require.NoError(t, rdr.Close())
	// Second close is idempotent.
	require.NoError(t, rdr.Close())
}

// cancelableStdinReader.Read — Poll wakes on a byte available on the
// underlying fd; Read returns (1, nil) with the byte. The slave PTY
// starts in canonical (line-buffered) mode; a single byte without
// newline never reaches a Read until we put the slave into cbreak so
// VMIN=1 makes bytes available immediately.
func TestCancelableStdinReader_ReadByteFromPTY(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	oldState, err := makeCbreak(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = term.Restore(int(slave.Fd()), oldState) })

	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = master.Write([]byte("x"))
	}()

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, byte('x'), buf[0])
}

// cancelableStdinReader.Read — closing the self-pipe's write end fires
// POLLHUP on the read end, which makes Read return io.EOF without ever
// consuming a byte from the underlying fd. We close only the write end
// (not the full rdr.Close()) so cancelR remains a valid fd through
// the wakeup; production's startKeyboardListener does the same thing
// implicitly because rdr.Close() closes cancelW before cancelR.
func TestCancelableStdinReader_ReadCancelledViaClose(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	// Snapshot cancelW so we can close just it without nilling fields.
	cancelW := rdr.cancelW

	// Trigger cancellation in a goroutine after a short delay.
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = cancelW.Close()
	}()

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	assert.Equal(t, 0, n)
	assert.True(t, errors.Is(err, io.EOF), "Read should return io.EOF after the cancel pipe's write end closes")
}

// makeCbreak IoctlGetTermios error — drive via the seam.
func TestMakeCbreak_IoctlGetTermiosError(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	orig := unixIoctlGetTermiosFn
	unixIoctlGetTermiosFn = func(int, uint) (*unix.Termios, error) {
		return nil, errors.New("ioctl get boom")
	}
	t.Cleanup(func() { unixIoctlGetTermiosFn = orig })

	_, err = makeCbreak(int(slave.Fd()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// makeCbreak IoctlSetTermios error — drive via the seam.
func TestMakeCbreak_IoctlSetTermiosError(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	orig := unixIoctlSetTermiosFn
	unixIoctlSetTermiosFn = func(int, uint, *unix.Termios) error {
		return errors.New("ioctl set boom")
	}
	t.Cleanup(func() { unixIoctlSetTermiosFn = orig })

	_, err = makeCbreak(int(slave.Fd()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// newCancelableStdinReader os.Pipe error — drive via the seam.
func TestNewCancelableStdinReader_PipeError(t *testing.T) {
	orig := osPipeFn
	osPipeFn = func() (*os.File, *os.File, error) {
		return nil, nil, errors.New("pipe boom")
	}
	t.Cleanup(func() { osPipeFn = orig })

	_, err := newCancelableStdinReader(0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// cancelableStdinReader.Read Poll EINTR continue — drive via the seam.
// First Poll returns EINTR (continue), second returns POLLHUP on cancelR.
func TestCancelableStdinReader_ReadPollEINTRContinue(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	calls := 0
	orig := unixPollFn
	unixPollFn = func(fds []unix.PollFd, timeout int) (int, error) {
		calls++
		if calls == 1 {
			return -1, unix.EINTR
		}
		// Second call: signal cancellation via POLLHUP on fds[1].
		fds[1].Revents = unix.POLLHUP
		return 1, nil
	}
	t.Cleanup(func() { unixPollFn = orig })

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	assert.Equal(t, 0, n)
	assert.True(t, errors.Is(err, io.EOF))
	assert.GreaterOrEqual(t, calls, 2, "should have retried after EINTR")
}

// cancelableStdinReader.Read Poll non-EINTR error — returns the error.
func TestCancelableStdinReader_ReadPollError(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	orig := unixPollFn
	unixPollFn = func([]unix.PollFd, int) (int, error) {
		return -1, errors.New("poll boom")
	}
	t.Cleanup(func() { unixPollFn = orig })

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	assert.Equal(t, 0, n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// cancelableStdinReader.Read unix.Read returns EINTR → loop retries.
func TestCancelableStdinReader_ReadEINTRRetry(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	// Poll always reports stdin (fds[0]) has POLLIN.
	origPoll := unixPollFn
	unixPollFn = func(fds []unix.PollFd, _ int) (int, error) {
		fds[0].Revents = unix.POLLIN
		return 1, nil
	}
	t.Cleanup(func() { unixPollFn = origPoll })

	// unix.Read returns EINTR first, then succeeds with one byte.
	calls := 0
	origRead := unixReadFn
	unixReadFn = func(fd int, p []byte) (int, error) {
		calls++
		if calls == 1 {
			return -1, unix.EINTR
		}
		p[0] = 'z'
		return 1, nil
	}
	t.Cleanup(func() { unixReadFn = origRead })

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, byte('z'), buf[0])
	assert.GreaterOrEqual(t, calls, 2, "should have retried after EINTR")
}

// cancelableStdinReader.Read unix.Read returns non-EINTR error.
func TestCancelableStdinReader_ReadReadError(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	origPoll := unixPollFn
	unixPollFn = func(fds []unix.PollFd, _ int) (int, error) {
		fds[0].Revents = unix.POLLIN
		return 1, nil
	}
	t.Cleanup(func() { unixPollFn = origPoll })

	origRead := unixReadFn
	unixReadFn = func(int, []byte) (int, error) {
		return -1, errors.New("read boom")
	}
	t.Cleanup(func() { unixReadFn = origRead })

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	assert.Equal(t, 0, n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// cancelableStdinReader.Read unix.Read returns 0 bytes → EOF.
func TestCancelableStdinReader_ReadZeroBytesIsEOF(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	origPoll := unixPollFn
	unixPollFn = func(fds []unix.PollFd, _ int) (int, error) {
		fds[0].Revents = unix.POLLIN
		return 1, nil
	}
	t.Cleanup(func() { unixPollFn = origPoll })

	origRead := unixReadFn
	unixReadFn = func(int, []byte) (int, error) { return 0, nil }
	t.Cleanup(func() { unixReadFn = origRead })

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	assert.Equal(t, 0, n)
	assert.True(t, errors.Is(err, io.EOF))
}

// cancelableStdinReader.Read spurious wakeup on stdin (POLLERR) — line 135-137.
func TestCancelableStdinReader_ReadSpuriousWakeupReturnsEOF(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	orig := unixPollFn
	unixPollFn = func(fds []unix.PollFd, _ int) (int, error) {
		fds[0].Revents = unix.POLLERR // not POLLIN, but non-zero → spurious
		return 1, nil
	}
	t.Cleanup(func() { unixPollFn = orig })

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	assert.Equal(t, 0, n)
	assert.True(t, errors.Is(err, io.EOF))
}

// cancelableStdinReader.Read — after Close has nil'd cancelR, the
// next Read returns io.EOF without trying to poll a closed file.
func TestCancelableStdinReader_ReadAfterCloseReturnsEOF(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	require.NoError(t, rdr.Close())

	buf := make([]byte, 1)
	n, err := rdr.Read(buf)
	assert.Equal(t, 0, n)
	assert.True(t, errors.Is(err, io.EOF))
}

// cancelableStdinReader.Read — zero-length buffer is a no-op.
func TestCancelableStdinReader_ReadEmptyBuffer(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	rdr, err := newCancelableStdinReader(int(slave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdr.Close() })

	n, err := rdr.Read(nil)
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
}
