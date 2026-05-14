//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package commands

import "golang.org/x/sys/unix"

// On Darwin and the BSDs the ioctl pair that reads / writes the
// termios struct is TIOCGETA / TIOCSETA. Linux uses different names
// (TCGETS / TCSETS), so the constants live behind a build tag and the
// shared makeCbreak references the alias.
const (
	ioctlGetTermiosReq = unix.TIOCGETA
	ioctlSetTermiosReq = unix.TIOCSETA
)
