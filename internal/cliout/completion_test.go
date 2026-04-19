package cliout

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Completion coverage for cliout.PrintJSON — the "always JSON even
// in text mode" path used by commands with JSON output semantics
// that the global --json flag shouldn't override.
// ─────────────────────────────────────────────────────────────────────

// TestPrintJSON_AlwaysJSON — PrintJSON writes JSON regardless of the
// global JSON mode. We verify by setting text mode and confirming
// the output still parses.
func TestPrintJSON_AlwaysJSON(t *testing.T) {
	// Swap stdout for the duration of the call so we can capture
	// what PrintJSON emits. cliout.PrintJSON writes to os.Stdout
	// directly — the same pattern other tests in this tree use.
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	// Put cliout in text mode so we're specifically testing that
	// PrintJSON ignores it.
	saved := JSON()
	SetJSONMode(false)
	t.Cleanup(func() { SetJSONMode(saved) })

	PrintJSON(map[string]string{"k": "v"})
	_ = w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	assert.Contains(t, buf.String(), `"k":"v"`)
	assert.True(t, strings.HasSuffix(buf.String(), "\n"))
}
