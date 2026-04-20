package commands

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForwardInterrupt_NilProcess — signal fired with no process
// running; helper returns cleanly.
func TestForwardInterrupt_NilProcess(t *testing.T) {
	sigChan := make(chan os.Signal, 1)
	sigChan <- os.Interrupt
	forwardInterrupt(sigChan, func() *os.Process { return nil })
}

// TestForwardInterrupt_WithProcess — signal fired with a running
// process; helper calls Signal on it.
func TestForwardInterrupt_WithProcess(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() })
	sigChan := make(chan os.Signal, 1)
	sigChan <- os.Interrupt
	forwardInterrupt(sigChan, func() *os.Process { return cmd.Process })
}

// TestConsoleProcFn — exercises the closure body via the seam.
func TestConsoleProcFn(t *testing.T) {
	cmd := exec.Command("true")
	fn := consoleProcFn(cmd)
	// Before Start, cmd.Process is nil; after Run it populates.
	assert.Nil(t, fn())
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() })
	assert.NotNil(t, fn())
}
