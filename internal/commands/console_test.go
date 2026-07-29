package commands

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsoleCmd_RunE(t *testing.T) {
	orig := execLookPath
	execLookPath = func(name string) (string, error) { return "/fake/yaegi", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 0)
	assert.NoError(t, consoleCmd.RunE(consoleCmd, nil))
}

func TestRunConsole_YaegiNotFound(t *testing.T) {
	orig := execLookPath
	execLookPath = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { execLookPath = orig })

	err := runConsole()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "yaegi")
}

func TestRunConsole_FakeSuccess(t *testing.T) {
	orig := execLookPath
	execLookPath = func(name string) (string, error) { return "/fake/yaegi", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 0)

	assert.NoError(t, runConsole())
}

func TestConsoleCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "console" {
			found = true
			break
		}
	}
	assert.True(t, found, "consoleCmd should be registered on rootCmd")
}

func TestConsoleCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, consoleCmd.Short)
	assert.NotEmpty(t, consoleCmd.Long)
}

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

// TestConsole_JSONModeRefuses — console is a REPL; it must refuse with
// CodeInteractiveOnly in JSON mode rather than launching yaegi (whose
// interactive output would corrupt the JSON stream).
func TestConsole_JSONModeRefuses(t *testing.T) {
	withJSONMode(t)
	err := runConsole()
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInteractiveOnly), ce.Code)
	assert.Contains(t, strings.ToLower(ce.Message), "console")
}
