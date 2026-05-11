//go:build linux

package commands

import "golang.org/x/sys/unix"

// On Linux the ioctl pair that reads / writes the termios struct is
// TCGETS / TCSETS. Darwin and the BSDs use different names (TIOCGETA /
// TIOCSETA), so the constants live behind a build tag and the shared
// makeCbreak references the alias.
const (
	ioctlGetTermiosReq = unix.TCGETS
	ioctlSetTermiosReq = unix.TCSETS
)
