package cliout

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// withStdouterr swaps os.Stdout and os.Stderr for pipes for the
// duration of fn, then returns the captured stdout and stderr text.
// Centralizes the dance so each helper test reads in two lines.
func withStdouterr(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut := os.Stdout
	origErr := os.Stderr
	t.Cleanup(func() {
		os.Stdout = origOut
		os.Stderr = origErr
	})

	rOut, wOut, err := os.Pipe()
	require.NoError(t, err)
	rErr, wErr, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = wOut
	os.Stderr = wErr

	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, rOut) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, rErr) }()

	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	wg.Wait()
	return outBuf.String(), errBuf.String()
}

// withJSONMode flips cliout into JSON mode and restores on cleanup.
func withJSONMode(t *testing.T) {
	t.Helper()
	SetJSONMode(true)
	t.Cleanup(func() { SetJSONMode(false) })
}
