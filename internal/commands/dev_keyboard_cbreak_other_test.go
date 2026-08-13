//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly)

package commands

// killSelfWithSIGINT stub for platforms without syscall.Kill (windows).
// The signaled-exit tests that depend on real signal delivery only run
// on unix; this exists so the shared TestHelperProcess compiles
// everywhere.
func killSelfWithSIGINT() {}
