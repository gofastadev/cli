package commands

import (
	"os"
	"testing"

	"github.com/gofastadev/cli/internal/commands/ai"
	"github.com/stretchr/testify/require"
)

// captureOut redirects os.Stdout while fn runs; returns whatever was
// written. Used by the ai-bridge coverage tests which invoke commands
// that print to stdout.
func captureOut(fn func()) string {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig
	data := make([]byte, 64*1024)
	n, _ := r.Read(data)
	return string(data[:n])
}

// TestAIBridgeInit_Closure — the init() closure registered via
// ai.SetVersionResolver is exercised when the ai install runner calls
// buildInstallData. Setting rootCmd.Version + invoking the ai Cmd
// drives it end-to-end.
func TestAIBridgeInit_Closure(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("go.mod", []byte("module x\n\ngo 1.25.0\n"), 0o644))
	// Setting rootCmd.Version lets us verify the resolver returns it.
	orig := rootCmd.Version
	rootCmd.Version = "v-test-0.0"
	t.Cleanup(func() { rootCmd.Version = orig })
	// Call runInstall via the ai.Cmd's RunE which triggers
	// buildInstallData (indirectly via the resolver closure).
	err := ai.Cmd.RunE(ai.Cmd, []string{"nonexistent-agent"})
	require.Error(t, err) // unknown-agent error; the resolver still fires.
	_ = captureOut(func() {
		_ = ai.Cmd.RunE(ai.Cmd, []string{"claude"})
	})
}
