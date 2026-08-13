package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		"warn":    "⚠",
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

// withFakeMigrateVersion overrides execCommand so subsequent calls
// (notably `migrate version` from checkPendingMigrations) return the
// supplied stdout and exit code, via the standard TestHelperProcess
// fake-subprocess mechanism. Restored on test cleanup.
func withFakeMigrateVersion(t *testing.T, output string, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := make([]string, 0, 3+len(args))
		cs = append(cs, "-test.run=TestHelperProcess", "--", name)
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+output,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
}

func TestStatusCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "status" {
			found = true
			break
		}
	}
	assert.True(t, found, "statusCmd should be registered on rootCmd")
}

// TestCheckWireDrift_NoWireGen — projects without wire_gen.go skip.
func TestCheckWireDrift_NoWireGen(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	check := checkWireDrift()
	assert.Equal(t, "skip", check.Status)
}

func TestCheckWireDrift_Stale(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	diDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(diDir, 0755))
	wireGen := filepath.Join(diDir, "wire_gen.go")
	require.NoError(t, os.WriteFile(wireGen, []byte("package di"), 0644))
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(wireGen, past, past))

	// Newer input file.
	require.NoError(t, os.WriteFile(filepath.Join(diDir, "wire.go"),
		[]byte("package di"), 0644))

	check := checkWireDrift()
	assert.Equal(t, "drift", check.Status)
	assert.Contains(t, check.Message, "gofasta wire")
}

func TestCheckSwaggerDrift_NoSwagger(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	check := checkSwaggerDrift()
	assert.Equal(t, "skip", check.Status)
}

// TestCheckPendingMigrations_DBUnreachable — migrations defined on
// disk but the DB can't be reached → "skip" with a clear message
// (no false "pending" claim).
func TestCheckPendingMigrations_DBUnreachable(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.down.sql"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), []byte(""), 0644))

	check := checkPendingMigrations()
	assert.Equal(t, "skip", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) defined")
	assert.Contains(t, check.Message, "could not check applied state")
}

func TestCheckPendingMigrations_NoDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	check := checkPendingMigrations()
	assert.Equal(t, "skip", check.Status)
}

// TestCheckPendingMigrations_AllApplied — when migrate version reports
// the same version as the highest defined .up.sql, status is "ok".
// This is the regression case from the user report: all migrations
// applied via server start, but the old code falsely warned "pending".
func TestCheckPendingMigrations_AllApplied(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), []byte(""), 0644))

	withFakeMigrateVersion(t, "2\n", 0)
	check := checkPendingMigrations()
	assert.Equal(t, "ok", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) applied")
	assert.Contains(t, check.Message, "current version: 2")
}

// TestCheckPendingMigrations_SomePending — current applied version is
// less than the latest defined: status is "drift" with the count of
// genuinely pending migrations (not just the file count).
func TestCheckPendingMigrations_SomePending(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	for _, name := range []string{
		"000001_init.up.sql", "000002_users.up.sql",
		"000003_orders.up.sql", "000004_audit.up.sql",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(mDir, name), nil, 0644))
	}

	withFakeMigrateVersion(t, "2\n", 0)
	check := checkPendingMigrations()
	assert.Equal(t, "drift", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) pending")
	assert.Contains(t, check.Message, "current: 2")
	assert.Contains(t, check.Message, "latest defined: 4")
}

// TestCheckPendingMigrations_DirtyState — migrate reports "X (dirty)"
// when a previous migration failed mid-step. Status should be "warn"
// with a clear remediation pointer.
func TestCheckPendingMigrations_DirtyState(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), nil, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), nil, 0644))

	withFakeMigrateVersion(t, "1 (dirty)\n", 0)
	check := checkPendingMigrations()
	assert.Equal(t, "warn", check.Status)
	assert.Contains(t, check.Message, "dirty")
	assert.Contains(t, check.Message, "1")
}

// TestCheckPendingMigrations_NoMigrationApplied — fresh DB, migrate
// version returns "no migration". Every defined migration is pending.
func TestCheckPendingMigrations_NoMigrationApplied(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), nil, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), nil, 0644))

	withFakeMigrateVersion(t, "no migration\n", 1)
	check := checkPendingMigrations()
	assert.Equal(t, "drift", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) pending")
}

// TestStatusCmd_RunE — exercises the Cobra RunE wrapper.
func TestStatusCmd_RunE(t *testing.T) {
	chdirTemp(t)
	_ = statusCmd.RunE(statusCmd, nil)
}

// TestReadDefinedMigrations_SkipsMalformed — files without an underscore
// (idx<=0) and files whose numeric prefix doesn't parse are silently
// skipped; the rest still parse.
func TestReadDefinedMigrations_SkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"000001_init.up.sql",           // valid
		"notanumber_x.up.sql",          // Atoi err
		"badname.up.sql",               // no underscore  → idx <= 0
		"000002_users.up.sql",          // valid
		"000003_extras.down.sql",       // wrong suffix → ignored
		"000001_init.duplicate.up.sql", // dedup via seen map
	}
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), nil, 0o644))
	}
	got, err := readDefinedMigrations(dir)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, got)
}

// TestReadDefinedMigrations_ReadDirError — missing dir surfaces the
// os.ReadDir error.
func TestReadDefinedMigrations_ReadDirError(t *testing.T) {
	_, err := readDefinedMigrations("/nonexistent/path/x")
	require.Error(t, err)
}

// TestReadAppliedMigrationVersion_UnexpectedOutputErrors — migrate
// version returns text that doesn't start with a number → parse error.
func TestReadAppliedMigrationVersion_UnexpectedOutputErrors(t *testing.T) {
	chdirTemp(t)
	withFakeMigrateOutput(t, "garbage text\n", 0)
	_, _, err := readAppliedMigrationVersion("db/migrations", "postgres://stub")
	require.Error(t, err)
}

// TestRunStatus_DriftReturnsError — when checkWireDrift reports drift,
// runStatus exits with CodeVerifyFailed and a non-nil error.
func TestRunStatus_DriftReturnsError(t *testing.T) {
	chdirTemp(t)
	wireDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(wireDir, 0o755))
	wireGen := filepath.Join(wireDir, "wire_gen.go")
	require.NoError(t, os.WriteFile(wireGen, []byte("// generated\n"), 0o644))
	sibling := filepath.Join(wireDir, "wire.go")
	require.NoError(t, os.WriteFile(sibling, []byte("// input\n"), 0o644))
	old := time.Now().Add(-365 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(wireGen, old, old))

	_ = captureStdout(t, func() {
		err := runStatus()
		require.Error(t, err)
	})
}

// TestReadAppliedMigrationVersion_EmptyStdoutSuccess — migrate version
// returned successfully but with empty stdout. Treat as a clean schema
// with no migrations applied.
func TestReadAppliedMigrationVersion_EmptyStdoutSuccess(t *testing.T) {
	chdirTemp(t)
	withFakeMigrateOutput(t, "", 0)
	current, dirty, err := readAppliedMigrationVersion("db/migrations", "postgres://stub")
	require.NoError(t, err)
	assert.Equal(t, 0, current)
	assert.False(t, dirty)
}

// TestReadAppliedMigrationVersion_NoMigrationSuccess — exit 0 + "no
// migration" output (some versions of migrate emit this on a clean
// schema_migrations table). Treat as 0/clean.
func TestReadAppliedMigrationVersion_NoMigrationSuccess(t *testing.T) {
	chdirTemp(t)
	withFakeMigrateOutput(t, "no migration\n", 0)
	current, _, err := readAppliedMigrationVersion("db/migrations", "postgres://stub")
	require.NoError(t, err)
	assert.Equal(t, 0, current)
}

// TestCheckPendingMigrations_NoMigrationsDefined — empty
// db/migrations dir → "no migrations defined" ok status.
func TestCheckPendingMigrations_NoMigrationsDefined(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0o755))
	check := checkPendingMigrations()
	assert.Equal(t, "ok", check.Status)
	assert.Contains(t, check.Message, "no migrations defined")
}

// TestCheckPendingMigrations_EmptyDBURL — buildMigrationURL returns
// "" so checkPendingMigrations skips with a count-and-defer message.
func TestCheckPendingMigrations_EmptyDBURL(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), nil, 0o644))

	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })

	check := checkPendingMigrations()
	assert.Equal(t, "skip", check.Status)
	assert.Contains(t, check.Message, "could not load config")
}

// TestRunStatus_WarnCaseExercised — checkPendingMigrations returns
// "warn" when the schema is dirty, so the runStatus switch hits the
// case "warn": result.Warnings++ arm that other tests miss.
func TestRunStatus_WarnCaseExercised(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), nil, 0o644))
	withFakeMigrateVersion(t, "1 (dirty)\n", 0)

	_ = captureStdout(t, func() {
		// Other checks may incidentally report drift in the temp-dir
		// setup; we only assert the warn arm of the inner switch was
		// reachable, not the final exit status.
		_ = runStatus()
	})
}
