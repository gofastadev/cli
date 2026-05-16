package commands

import (
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// test.go — runTests JSON-mode failure path
// ─────────────────────────────────────────────────────────────────────

// TestRunTests_JSONMode_Failure — JSON mode's c.Stderr = os.Stderr
// branch + the c.Run() failure → CodeGoTestFailed path. Covers the
// "if opts.jsonMode" block of runTests separately from text mode.
func TestRunTests_JSONMode_Failure(t *testing.T) {
	stagedFakeExec(t, 1)
	stubExecLookPathOK(t)
	err := runTests(testOptions{jsonMode: true})
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeGoTestFailed), ce.Code)
}

// TestRunTests_JSONMode_Success — JSON mode happy path: c.Run()
// returns nil so the function returns nil and skips
// printCoverageTotal even with coverage=true (NDJSON contract).
func TestRunTests_JSONMode_Success(t *testing.T) {
	stagedFakeExec(t, 0)
	stubExecLookPathOK(t)
	assert.NoError(t, runTests(testOptions{jsonMode: true, coverage: true}))
}
