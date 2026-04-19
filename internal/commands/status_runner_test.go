package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for status.go entry points that the existing status_test.go
// doesn't already reach: runStatus end-to-end, statusMark (every
// branch), checkSwaggerDrift's drift + ok branches, checkGoSumFreshness
// skip path, checkUncommittedGenerated's not-a-repo path.
// ─────────────────────────────────────────────────────────────────────

// chdirStatusTemp creates + chdir's to a fresh temp dir for the test.
func chdirStatusTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
}

func TestStatusMark_EveryBranch(t *testing.T) {
	cases := map[string]string{
		"ok":      "✓",
		"drift":   "✗",
		"warn":    "!",
		"skip":    "-",
		"unknown": "?",
	}
	for status, want := range cases {
		got := stripANSI(statusMark(status))
		assert.Equal(t, want, got, "status=%s", status)
	}
}

// TestCheckSwaggerDrift_InSync — swagger.json newer than every
// controller → "ok".
func TestCheckSwaggerDrift_InSync(t *testing.T) {
	dir := chdirStatusTemp(t)
	controllersDir := filepath.Join(dir, "app", "rest", "controllers")
	require.NoError(t, os.MkdirAll(controllersDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(controllersDir, "x.go"), []byte("package controllers"), 0o644))
	// Pin controller mtime to the past, swagger to "now".
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(
		filepath.Join(controllersDir, "x.go"), past, past))
	swagger := filepath.Join(dir, "docs", "swagger.json")
	require.NoError(t, os.WriteFile(swagger, []byte("{}"), 0o644))

	got := checkSwaggerDrift()
	assert.Equal(t, "ok", got.Status)
}

// TestCheckSwaggerDrift_Drift — controller newer than swagger.json
// → "drift" with a remediation hint.
func TestCheckSwaggerDrift_Drift(t *testing.T) {
	dir := chdirStatusTemp(t)
	controllersDir := filepath.Join(dir, "app", "rest", "controllers")
	require.NoError(t, os.MkdirAll(controllersDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))
	swagger := filepath.Join(dir, "docs", "swagger.json")
	require.NoError(t, os.WriteFile(swagger, []byte("{}"), 0o644))
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(swagger, past, past))
	require.NoError(t, os.WriteFile(
		filepath.Join(controllersDir, "user.controller.go"),
		[]byte("package controllers"), 0o644))

	got := checkSwaggerDrift()
	assert.Equal(t, "drift", got.Status)
	assert.Contains(t, got.Message, "gofasta swagger")
}

// TestCheckGoSumFreshness_RunsGoVerify — the check invokes
// `go mod verify`. In a temp dir with no go.mod it surfaces as
// "drift" (verify exits non-zero). We just verify the function
// returns a valid statusCheck with a non-ok status rather than
// panicking.
func TestCheckGoSumFreshness_RunsGoVerify(t *testing.T) {
	chdirStatusTemp(t)
	got := checkGoSumFreshness()
	// Without go.mod present, go mod verify fails → "drift".
	// If Go isn't on $PATH for some reason, it'd still not be "ok".
	assert.NotEmpty(t, got.Status)
	assert.NotEqual(t, "ok", got.Status)
}

// TestCheckUncommittedGenerated_NotARepo — temp dir is not a git
// repo. Depending on git's behavior the check either skips or
// reports ok; just assert it doesn't panic and doesn't incorrectly
// claim drift.
func TestCheckUncommittedGenerated_NotARepo(t *testing.T) {
	chdirStatusTemp(t)
	got := checkUncommittedGenerated()
	assert.NotEqual(t, "drift", got.Status)
}

// TestRunStatus_RunsEveryCheck — empty temp dir still executes every
// check and finishes without panicking. Exit code may be non-zero
// (go mod verify fails without a go.mod) but the runner itself
// completed the whole pipeline — that's what we're covering here.
func TestRunStatus_RunsEveryCheck(t *testing.T) {
	chdirStatusTemp(t)
	_ = runStatus() // may or may not error; the function runs either way
}

// TestRunStatus_DriftExitsNonZero — induced swagger drift makes
// runStatus return an error wrapping VERIFY_FAILED.
func TestRunStatus_DriftExitsNonZero(t *testing.T) {
	dir := chdirStatusTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "app", "rest", "controllers"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))
	swagger := filepath.Join(dir, "docs", "swagger.json")
	require.NoError(t, os.WriteFile(swagger, []byte("{}"), 0o644))
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(swagger, past, past))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "app", "rest", "controllers", "x.go"),
		[]byte("package controllers"), 0o644))

	err := runStatus()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drift")
}
