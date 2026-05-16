package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// db.go — JSON mode + summary branches
// ─────────────────────────────────────────────────────────────────────

// TestRunDBReset_JSON_Success — JSON mode emits a dbResetResult
// document; runDBStep's JSON branch (buffered stdout/stderr) is
// exercised too.
func TestRunDBReset_JSON_Success(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	withJSONMode(t)

	out := captureStdout(t, func() {
		require.NoError(t, runDBReset(true))
	})

	var got dbResetResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.NotEmpty(t, got.Steps)
}

// TestRunDBStep_JSONMode — runDBStep specifically: in JSON mode no
// step banner is printed and stdout/stderr are bagged in a buffer.
func TestRunDBStep_JSONMode(t *testing.T) {
	withJSONMode(t)
	withFakeExec(t, 0)
	require.NoError(t, runDBStep("test", "true"))
}

// TestPrintDBResetSummary_AllStatuses — exercises ok/fail/skip
// switch branches plus the final tally line.
func TestPrintDBResetSummary_AllStatuses(t *testing.T) {
	r := dbResetResult{
		Steps: []dbResetStep{
			{Name: "drop", Status: "ok", DurationMs: 12},
			{Name: "seed", Status: "fail", Message: "boom"},
			{Name: "extra", Status: "skip"},
		},
		DurationMs: 50,
	}
	var buf bytes.Buffer
	printDBResetSummary(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "drop complete")
	assert.Contains(t, out, "seed failed: boom")
	assert.Contains(t, out, "extra skipped")
	assert.Contains(t, out, "3 step(s)")
}
