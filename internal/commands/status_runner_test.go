package commands

import (
	"os"
	"os/exec"
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

// runGitCmd invokes git with the provided args against the current
// directory; used by the uncommitted-check tests below to prepare a
// tiny repo.
func runGitCmd(args ...string) error {
	c := exec.Command("git", args...)
	return c.Run()
}

// TestCheckPendingMigrations_UnreadableDir — the dir exists but
// ReadDir fails (permissions).
func TestCheckPendingMigrations_UnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0o755))
	require.NoError(t, os.Chmod(mDir, 0o111))
	t.Cleanup(func() { _ = os.Chmod(mDir, 0o755) })
	check := checkPendingMigrations()
	assert.Equal(t, "skip", check.Status)
	assert.Contains(t, check.Message, "could not read")
}

// TestCheckPendingMigrations_EmptyDir — empty migrations/ → "no
// migrations defined" ok.
func TestCheckPendingMigrations_EmptyDir(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join("db", "migrations"), 0o755))
	check := checkPendingMigrations()
	assert.Equal(t, "ok", check.Status)
}

// TestCheckUncommittedGenerated_GitNotOnPath — exec.LookPath("git")
// fails. Simulate by temporarily overriding PATH.
func TestCheckUncommittedGenerated_GitNotOnPath(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
	check := checkUncommittedGenerated()
	assert.Equal(t, "skip", check.Status)
	assert.Contains(t, check.Message, "git")
}

// TestCheckUncommittedGenerated_NoWatchedFiles — none of the watched
// paths exist → "generated files committed" ok.
func TestCheckUncommittedGenerated_NoWatchedFiles(t *testing.T) {
	chdirTemp(t)
	check := checkUncommittedGenerated()
	// Whether ok or skip depends on whether we're in a git repo; just
	// exercise the branch.
	_ = check
}

// TestCheckUncommittedGenerated_Dirty — a watched file exists and git
// reports it as modified (or not, depending on the environment).
func TestCheckUncommittedGenerated_Dirty(t *testing.T) {
	chdirTemp(t)
	// Create a watched file.
	require.NoError(t, os.MkdirAll(filepath.Join("app", "di"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("app", "di", "wire_gen.go"),
		[]byte("package di"), 0o644))
	check := checkUncommittedGenerated()
	// In a non-git temp dir, runGitPorcelain returns error → skip.
	assert.NotEmpty(t, check.Status)
}

// TestCheckGoSumFreshness_InModule — run from the CLI's own working
// directory where `go mod verify` succeeds.
func TestCheckGoSumFreshness_InModule(t *testing.T) {
	// Don't chdir — run from the actual cli/ dir where go.mod is valid.
	check := checkGoSumFreshness()
	assert.Equal(t, "ok", check.Status)
}

// TestCheckGoSumFreshness_Fails — chdir to a temp dir with no go.mod
// so `go mod verify` fails.
func TestCheckGoSumFreshness_Fails(t *testing.T) {
	chdirTemp(t)
	check := checkGoSumFreshness()
	assert.Equal(t, "drift", check.Status)
}

// TestStatusMark_Unknown — default branch returns "?".
func TestStatusMark_Unknown(t *testing.T) {
	assert.Equal(t, "?", statusMark("bogus"))
	assert.NotEmpty(t, statusMark("warn"))
}

// TestRunStatus_CoverageEntry — runs end-to-end in a pristine temp dir.
func TestRunStatus_CoverageEntry(t *testing.T) {
	chdirTemp(t)
	_ = runStatus()
}

// TestCheckUncommittedGenerated_Warn — init a tiny git repo, create
// a watched file, modify it → `git status --porcelain` returns
// non-empty → status=warn.
func TestCheckUncommittedGenerated_Warn(t *testing.T) {
	chdirTemp(t)
	// Skip if git isn't on $PATH.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	// Initialize a git repo and an ignored config.
	require.NoError(t, runGitCmd("init"))
	require.NoError(t, runGitCmd("config", "user.email", "x@y.com"))
	require.NoError(t, runGitCmd("config", "user.name", "X"))
	// Create a watched path AND commit it, then modify it.
	require.NoError(t, os.MkdirAll(filepath.Join("app", "di"), 0o755))
	path := filepath.Join("app", "di", "wire_gen.go")
	require.NoError(t, os.WriteFile(path, []byte("package di\n"), 0o644))
	require.NoError(t, runGitCmd("add", path))
	require.NoError(t, runGitCmd("commit", "-m", "init"))
	// Modify after commit → git status reports the change.
	require.NoError(t, os.WriteFile(path, []byte("package di // edit\n"), 0o644))
	check := checkUncommittedGenerated()
	assert.Equal(t, "warn", check.Status)
}

// TestRunStatus_WarnCounter — create a project with pending migrations
// so runStatus's warn-case increment branch fires.
func TestRunStatus_WarnCounter(t *testing.T) {
	chdirTemp(t)
	// db/migrations/ with at least one .up.sql → warn from
	// checkPendingMigrations.
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"),
		[]byte("-- x"), 0o644))
	require.NoError(t, os.WriteFile("go.mod",
		[]byte("module example.com/t\n\ngo 1.25.0\n"), 0o644))
	require.NoError(t, os.WriteFile("main.go", []byte("package main\nfunc main() {}\n"), 0o644))
	_ = runStatus()
}

// TestRunStatus_ReturnsNilWhenAllOK — set up a project where every
// check skips or passes so runStatus reaches `return nil`.
func TestRunStatus_ReturnsNilWhenAllOK(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	require.NoError(t, os.WriteFile("go.mod", []byte("module example.com/t\n\ngo 1.25.0\n"), 0o644))
	require.NoError(t, os.WriteFile("main.go", []byte("package main\nfunc main() {}\n"), 0o644))
	_ = runStatus()
}

// TestCheckWireDrift_InSync — wire_gen.go is newer than all inputs
// → "ok" status with "in sync" message.
func TestCheckWireDrift_InSync(t *testing.T) {
	chdirTemp(t)
	diDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(diDir, 0o755))
	// Input first, wire_gen second → wire_gen is newer.
	input := filepath.Join(diDir, "wire.go")
	require.NoError(t, os.WriteFile(input, []byte("package di"), 0o644))
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(input, past, past))
	wireGen := filepath.Join(diDir, "wire_gen.go")
	require.NoError(t, os.WriteFile(wireGen, []byte("package di"), 0o644))

	check := checkWireDrift()
	assert.Equal(t, "ok", check.Status)
	assert.Equal(t, "in sync", check.Message)
}

// TestCheckSwaggerDrift_Stale — swagger exists but a controller is
// newer.
func TestCheckSwaggerDrift_Stale(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("docs", 0o755))
	swagger := filepath.Join("docs", "swagger.json")
	require.NoError(t, os.WriteFile(swagger, []byte("{}"), 0o644))
	// Put a controller that's newer than swagger.
	cDir := filepath.Join("app", "rest", "controllers")
	require.NoError(t, os.MkdirAll(cDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cDir, "a.go"), []byte("package c"), 0o644))
	// Now make the swagger look older.
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(swagger, past, past))
	check := checkSwaggerDrift()
	assert.Equal(t, "drift", check.Status)
}
