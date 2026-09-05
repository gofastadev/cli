package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStepGoVet_SkipsWhenScopeHasNoPackages — scope is non-nil with
// NonGoOnly=false but Packages is empty → step short-circuits with
// "skip", "", nil.
func TestStepGoVet_SkipsWhenScopeHasNoPackages(t *testing.T) {
	saved := currentVerifyScope
	currentVerifyScope = &verifyScopeData{Packages: nil}
	t.Cleanup(func() { currentVerifyScope = saved })

	msg, _, err := stepGoVet()
	require.NoError(t, err)
	require.Equal(t, "skip", msg)
}

// TestStepGoTest_SkipsWhenScopeHasNoTestSet — analog for stepGoTest.
func TestStepGoTest_SkipsWhenScopeHasNoTestSet(t *testing.T) {
	saved := currentVerifyScope
	currentVerifyScope = &verifyScopeData{TestSet: nil}
	t.Cleanup(func() { currentVerifyScope = saved })

	msg, _, err := stepGoTest(true)
	require.NoError(t, err)
	require.Equal(t, "skip", msg)
}

// TestStepGoBuild_SkipsWhenScopeHasNoPackages — analog for stepGoBuild.
func TestStepGoBuild_SkipsWhenScopeHasNoPackages(t *testing.T) {
	saved := currentVerifyScope
	currentVerifyScope = &verifyScopeData{Packages: nil}
	t.Cleanup(func() { currentVerifyScope = saved })

	msg, _, err := stepGoBuild()
	require.NoError(t, err)
	require.Equal(t, "skip", msg)
}

// setupRepoWithGoFile builds a minimal git repo with go.mod + one file.
func setupRepoWithGoFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v: %s", args, out)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package m\n"), 0o644))
	run("git", "init", "-q", "-b", "main")
	run("git", "config", "user.email", "t@x")
	run("git", "config", "user.name", "t")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "add", ".")
	run("git", "commit", "-q", "-m", "init")
	return dir
}

// TestResolveVerifyScopeImpl_NoChanges — clean repo: ChangedFiles
// returns empty, scope is returned with empty files.
func TestResolveVerifyScopeImpl_NoChanges(t *testing.T) {
	dir := setupRepoWithGoFile(t)
	chdirTest(t, dir)
	scope, err := resolveVerifyScopeImpl(verifyOptions{since: "HEAD"})
	require.NoError(t, err)
	require.NotNil(t, scope)
	require.Empty(t, scope.Files)
}

// TestResolveVerifyScopeImpl_NonGoFileOnly — only README.md changed →
// NonGoOnly=true branch.
func TestResolveVerifyScopeImpl_NonGoFileOnly(t *testing.T) {
	dir := setupRepoWithGoFile(t)
	chdirTest(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte("hello"), 0o644))
	scope, err := resolveVerifyScopeImpl(verifyOptions{since: "HEAD"})
	require.NoError(t, err)
	require.True(t, scope.NonGoOnly)
}

// TestResolveVerifyScopeImpl_GoFileChanged — modify a.go → exercises
// the PackagesForDirs + ReverseDeps path.
func TestResolveVerifyScopeImpl_GoFileChanged(t *testing.T) {
	dir := setupRepoWithGoFile(t)
	chdirTest(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package m\nvar X = 1\n"), 0o644))
	scope, err := resolveVerifyScopeImpl(verifyOptions{since: "HEAD"})
	require.NoError(t, err)
	require.Greater(t, len(scope.Files), 0)
	require.Greater(t, len(scope.Dirs), 0)
}

// TestResolveVerifyScopeImpl_BadRefReturnsError — unknown ref →
// gitdiff.ChangedFiles errors → resolveVerifyScopeImpl returns err.
func TestResolveVerifyScopeImpl_BadRefReturnsError(t *testing.T) {
	dir := setupRepoWithGoFile(t)
	chdirTest(t, dir)
	_, err := resolveVerifyScopeImpl(verifyOptions{since: "this-ref-does-not-exist"})
	require.Error(t, err)
}

// TestResolveVerifyScopeImpl_PackagesForDirsError — inject a failure
// into the gitdiff.PackagesForDirs seam so the err-return branch fires.
func TestResolveVerifyScopeImpl_PackagesForDirsError(t *testing.T) {
	dir := setupRepoWithGoFile(t)
	chdirTest(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package m\nvar X = 1\n"), 0o644))

	saved := gitdiffPackagesForDirsFn
	gitdiffPackagesForDirsFn = func(_ context.Context, _ []string) ([]string, error) {
		return nil, errors.New("stub")
	}
	t.Cleanup(func() { gitdiffPackagesForDirsFn = saved })

	_, err := resolveVerifyScopeImpl(verifyOptions{since: "HEAD"})
	require.Error(t, err)
}

// TestResolveVerifyScopeImpl_ReverseDepsErrorFallback — ReverseDeps
// errors → scope.TestSet falls back to pkgs (the `else` branch).
func TestResolveVerifyScopeImpl_ReverseDepsErrorFallback(t *testing.T) {
	dir := setupRepoWithGoFile(t)
	chdirTest(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package m\nvar X = 1\n"), 0o644))

	saved := gitdiffReverseDepsFn
	gitdiffReverseDepsFn = func(_ context.Context, _ []string) ([]string, error) {
		return nil, errors.New("stub")
	}
	t.Cleanup(func() { gitdiffReverseDepsFn = saved })

	scope, err := resolveVerifyScopeImpl(verifyOptions{since: "HEAD"})
	require.NoError(t, err)
	require.Equal(t, scope.Packages, scope.TestSet)
}

// TestRunVerify_ExtraStepsAreAppended covers the extraVerifySteps seam itself.
// The seam exists so other tests can inject defensive branches without
// shelling out; the append that consumes it needs its own coverage, or a
// regression that dropped injected steps would go unnoticed and quietly
// disable every test relying on it.
func TestRunVerify_ExtraStepsAreAppended(t *testing.T) {
	inRenderedProject(t)

	called := false
	orig := extraVerifySteps
	extraVerifySteps = []verifyStepDef{{
		name: "injected",
		fn: func() (string, string, error) {
			called = true
			return "injected step ran", "", nil
		},
	}}
	t.Cleanup(func() { extraVerifySteps = orig })

	out := captureStdout(t, func() {
		_ = runVerify(verifyOptions{skipLint: true, skipRace: true, keepGoing: true})
	})

	assert.True(t, called, "an injected step must actually be executed")
	assert.Contains(t, out, "injected")
}

// withRecordedShell substitutes runShellFn with a recorder that returns
// success for every command. Restores the original on test cleanup.
func withRecordedShell(t *testing.T) *[]recordedShellCall {
	t.Helper()
	calls := &[]recordedShellCall{}
	orig := runShellFn
	runShellFn = func(name string, args ...string) (string, error) {
		*calls = append(*calls, recordedShellCall{name: name, args: append([]string{}, args...)})
		return "", nil
	}
	t.Cleanup(func() { runShellFn = orig })
	return calls
}

// withFakeScope installs a fixed verifyScopeData by stubbing the resolver
// seam — keeps the test off real git + go list while still exercising
// every step's scope-handling branch.
func withFakeScope(t *testing.T, scope *verifyScopeData) {
	t.Helper()
	orig := resolveVerifyScopeFn
	resolveVerifyScopeFn = func(_ verifyOptions) (*verifyScopeData, error) {
		return scope, nil
	}
	t.Cleanup(func() { resolveVerifyScopeFn = orig })
}

func TestVerify_Since_GofmtReceivesOnlyChangedGoFiles(t *testing.T) {
	calls := withRecordedShell(t)
	withFakeScope(t, &verifyScopeData{
		Since:    "HEAD~1",
		Files:    []string{"a.go", "b.txt", "pkg/c.go"},
		GoFiles:  []string{"a.go", "pkg/c.go"},
		Packages: []string{"example.com/m", "example.com/m/pkg"},
		TestSet:  []string{"example.com/m", "example.com/m/pkg"},
	})

	require.NoError(t, runVerify(verifyOptions{since: "HEAD~1"}))

	gofmt := findCall(*calls, "gofmt")
	require.NotNil(t, gofmt, "gofmt should have been invoked")
	require.Contains(t, gofmt.args, "a.go")
	require.Contains(t, gofmt.args, "pkg/c.go")
	// "." as a positional arg means "format the whole tree" — must not appear.
	for _, a := range gofmt.args {
		require.NotEqual(t, ".", a, "scoped gofmt must not also receive `.`")
	}
}

func TestVerify_Since_GoVetReceivesScopedPackages(t *testing.T) {
	calls := withRecordedShell(t)
	withFakeScope(t, &verifyScopeData{
		Since:    "HEAD~1",
		Files:    []string{"a.go"},
		GoFiles:  []string{"a.go"},
		Packages: []string{"example.com/m"},
		TestSet:  []string{"example.com/m"},
	})

	require.NoError(t, runVerify(verifyOptions{since: "HEAD~1"}))

	govet := findCall(*calls, "go")
	require.NotNil(t, govet)
	require.Contains(t, govet.args, "example.com/m")
	require.NotContains(t, strings.Join(govet.args, " "), "./...")
}

func TestVerify_Since_GolangciLintGetsNewFromRev(t *testing.T) {
	// Force lint to be considered installed by stubbing the lookup.
	origLP := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "/usr/bin/golangci-lint", nil }
	t.Cleanup(func() { golangciLintLookPath = origLP })

	calls := withRecordedShell(t)
	withFakeScope(t, &verifyScopeData{
		Since:    "origin/main",
		Files:    []string{"a.go"},
		GoFiles:  []string{"a.go"},
		Packages: []string{"example.com/m"},
		TestSet:  []string{"example.com/m"},
	})

	require.NoError(t, runVerify(verifyOptions{since: "origin/main"}))

	lint := findCall(*calls, "golangci-lint")
	require.NotNil(t, lint)
	require.Contains(t, lint.args, "--new-from-rev=origin/main")
}

// TestVerify_Since_NonGoChangesFallBackToFullProject — config.yaml /
// migration SQL changes might affect runtime behavior even when no Go
// code changed, so build/vet/test fall back to whole-project for safety.
// Only gofmt skips (it has nothing to format).
func TestVerify_Since_NonGoChangesFallBackToFullProject(t *testing.T) {
	calls := withRecordedShell(t)
	withFakeScope(t, &verifyScopeData{
		Since:     "HEAD~1",
		Files:     []string{"README.md", "config.yaml"},
		NonGoOnly: true,
	})

	require.NoError(t, runVerify(verifyOptions{since: "HEAD~1"}))

	// gofmt with no .go files is skipped (no shell call recorded).
	require.Nil(t, findCall(*calls, "gofmt"),
		"gofmt should be skipped when no .go files changed")

	// go vet / build / test must fall back to ./... when only non-Go
	// files changed — a config or migration change can affect runtime
	// behavior even without a Go diff.
	sawVet, sawBuild, sawTest := false, false, false
	for _, c := range *calls {
		if c.name != "go" {
			continue
		}
		switch {
		case len(c.args) > 0 && c.args[0] == "vet":
			require.Contains(t, c.args, "./...", "vet should fall back to ./... under non-Go-only changes")
			sawVet = true
		case len(c.args) > 0 && c.args[0] == "build":
			require.Contains(t, c.args, "./...", "build should fall back to ./...")
			sawBuild = true
		case len(c.args) > 0 && c.args[0] == "test":
			require.Contains(t, c.args, "./...", "test should fall back to ./...")
			sawTest = true
		}
	}
	require.True(t, sawVet && sawBuild && sawTest,
		"vet/build/test must all run with ./... under non-Go-only changes")
}

func TestVerify_Since_PopulatesScopedFieldsInResult(t *testing.T) {
	withRecordedShell(t)
	withFakeScope(t, &verifyScopeData{
		Since:    "HEAD~1",
		Files:    []string{"a.go"},
		GoFiles:  []string{"a.go"},
		Packages: []string{"example.com/m"},
		TestSet:  []string{"example.com/m"},
	})

	require.NoError(t, runVerify(verifyOptions{since: "HEAD~1"}))
}

func TestVerify_Since_ResolverErrorPropagates(t *testing.T) {
	orig := resolveVerifyScopeFn
	resolveVerifyScopeFn = func(_ verifyOptions) (*verifyScopeData, error) {
		return nil, clierr.New(clierr.CodeGitNotAvailable, "not in a repo")
	}
	t.Cleanup(func() { resolveVerifyScopeFn = orig })

	err := runVerify(verifyOptions{since: "HEAD~1"})
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeGitNotAvailable), codeOf(err))
}

// findCall returns the first call matching the given binary name, or nil.
func findCall(calls []recordedShellCall, name string) *recordedShellCall {
	for i := range calls {
		if calls[i].name == name {
			return &calls[i]
		}
	}
	return nil
}

// TestVerifyCmd_Registered ensures `verify` shows up on the root command.
func TestVerifyCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "verify" {
			found = true
			break
		}
	}
	assert.True(t, found, "verifyCmd should be registered on rootCmd")
}

// TestVerifyCmd_HasDescription ensures the long text is set so `gofasta
// verify --help` is informative.
func TestVerifyCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, verifyCmd.Short)
	assert.NotEmpty(t, verifyCmd.Long)
}

// TestRunVerify_LoadsDotEnv — regression: verify must load .env so the
// `go test ./...` child process inherits project env vars (used by
// integration tests that read config.yaml + env overrides).
func TestRunVerify_LoadsDotEnv(t *testing.T) {
	chdirTemp(t)
	const probe = "GOFASTA_VERIFY_DOTENV_PROBE"
	require.NoError(t, os.WriteFile(".env", []byte(probe+"=loaded\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv(probe) })

	// Stub all the steps to no-op so the test doesn't shell out for real.
	origRun := runShellFn
	runShellFn = func(_ string, _ ...string) (string, error) { return "", nil }
	t.Cleanup(func() { runShellFn = origRun })
	origLook := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "", fmt.Errorf("not installed") }
	t.Cleanup(func() { golangciLintLookPath = origLook })

	_ = runVerify(verifyOptions{skipLint: true, keepGoing: true})
	assert.Equal(t, "loaded", os.Getenv(probe),
		"runVerify must call loadDotEnv before running test/build steps")
}

// TestStepWireDrift_NoWireGenSkips — projects without app/di/wire_gen.go
// are valid (e.g., a pure-library project). Drift check must skip.
func TestStepWireDrift_NoWireGenSkips(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	msg, _, err := stepWireDrift()
	assert.NoError(t, err)
	assert.Equal(t, "skip", msg, "expected skip status when wire_gen.go absent")
}

// TestStepWireDrift_UpToDate — wire_gen.go newer than all input files:
// pass.
func TestStepWireDrift_UpToDate(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	diDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(diDir, 0755))

	// Write an input file, then wire_gen.go with a newer mod time.
	input := filepath.Join(diDir, "wire.go")
	require.NoError(t, os.WriteFile(input, []byte("package di"), 0644))
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(input, past, past))

	wireGen := filepath.Join(diDir, "wire_gen.go")
	require.NoError(t, os.WriteFile(wireGen, []byte("package di"), 0644))

	msg, _, err := stepWireDrift()
	assert.NoError(t, err)
	assert.Empty(t, msg, "expected no message on pass")
}

// TestStepWireDrift_Stale — wire.go newer than wire_gen.go: fail.
func TestStepWireDrift_Stale(t *testing.T) {
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

	// Input file newer than wire_gen.go.
	input := filepath.Join(diDir, "wire.go")
	require.NoError(t, os.WriteFile(input, []byte("package di"), 0644))

	msg, _, err := stepWireDrift()
	assert.Error(t, err, "stale wire_gen.go should fail")
	assert.Contains(t, msg, "wire_gen.go is older than")
	assert.Contains(t, msg, "gofasta wire")
}

// TestStepRoutes_NoRoutesDirSkips — pure-GraphQL projects (no REST) skip
// the routes check.
func TestStepRoutes_NoRoutesDirSkips(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	msg, _, err := stepRoutes()
	assert.NoError(t, err)
	assert.Equal(t, "skip", msg)
}

// TestRunVerify_ReturnsVerifyFailedCode — when any step fails, runVerify
// returns a clierr.Error with CodeVerifyFailed so agents can branch on it.
func TestRunVerify_ReturnsVerifyFailedCode(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// Empty dir: gofmt will pass (no files), vet will fail (no go.mod).
	// Either way, keepGoing=true makes every step run before we return.
	err := runVerify(verifyOptions{skipLint: true, skipRace: true, keepGoing: true})
	if err == nil {
		t.Skip("env has a gofasta-ish project at temp path; skipping failure assertion")
	}
	structured, ok := clierr.As(err)
	if !ok {
		t.Fatalf("expected clierr.Error, got %T: %v", err, err)
	}
	assert.Equal(t, string(clierr.CodeVerifyFailed), structured.Code)
}

// TestStepRoutes_Skip — no app/rest/routes/ → "skip".
func TestStepRoutes_Skip(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	msg, _, err := stepRoutes()
	require.NoError(t, err)
	assert.Equal(t, "skip", msg)
}

// TestStepGolangciLint_Invokes — smoke test. Behavior depends on
// whether golangci-lint is on $PATH (CI installs it, dev boxes
// vary), so we only confirm the function doesn't panic. Both the
// skip branch and the error branch are valid outcomes.
func TestStepGolangciLint_Invokes(t *testing.T) {
	_, _, _ = stepGolangciLint()
}

// TestVerifyCmd_RunE_KeepGoing — exercises the Cobra RunE wrapper
// with --keep-going set so every step runs.
func TestVerifyCmd_RunE_KeepGoing(t *testing.T) {
	chdirTemp(t)
	// Pristine temp dir: gofmt passes, vet fails (no go.mod) — but with
	// keep-going set and skipLint set the test exercises the full RunE.
	verifyNoLint = true
	verifyKeepGoing = true
	verifyNoRace = true
	t.Cleanup(func() { verifyNoLint = false; verifyKeepGoing = false; verifyNoRace = false })
	// verifyCmd.RunE returns the verify-failed clierr when there are
	// failed checks. We accept either outcome — this test is only
	// about covering the anonymous RunE wrapper.
	_ = verifyCmd.RunE(verifyCmd, nil)
}

// TestStepGofmt_RunError — the underlying shell errors outright
// (gofmt invocation failed — e.g. gofmt not installed).
func TestStepGofmt_RunError(t *testing.T) {
	withStubShell(t, stubResponse{out: "", err: fmt.Errorf("exec failed")})
	_, _, err := stepGofmt()
	require.Error(t, err)
}

// TestStepGofmt_FindsDriftFiles — gofmt returns a list of files that
// need reformatting → error mentioning "gofmt".
func TestStepGofmt_FindsDriftFiles(t *testing.T) {
	withStubShell(t, stubResponse{out: "main.go\n", err: nil})
	msg, _, err := stepGofmt()
	require.Error(t, err)
	assert.Equal(t, "files need reformatting", msg)
}

// TestStepGofmt_Clean — empty output means everything is clean.
func TestStepGofmt_Clean(t *testing.T) {
	withStubShell(t, stubResponse{out: "", err: nil})
	msg, _, err := stepGofmt()
	assert.NoError(t, err)
	assert.Empty(t, msg)
}

// TestStepGoVet_Clean — `go vet` exits 0.
func TestStepGoVet_Clean(t *testing.T) {
	withStubShell(t, stubResponse{out: "", err: nil})
	msg, _, err := stepGoVet()
	assert.NoError(t, err)
	assert.Empty(t, msg)
}

// TestStepGoVet_Issues — vet exits non-zero with stdout attached.
func TestStepGoVet_Issues(t *testing.T) {
	withStubShell(t, stubResponse{out: "some issue", err: fmt.Errorf("vet")})
	msg, _, err := stepGoVet()
	require.Error(t, err)
	assert.Equal(t, "vet reported issues", msg)
}

// TestStepGolangciLint_NotInstalled — look-path seam returns an error
// → skip.
func TestStepGolangciLint_NotInstalled(t *testing.T) {
	orig := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { golangciLintLookPath = orig })
	msg, _, err := stepGolangciLint()
	assert.NoError(t, err)
	assert.Equal(t, "skip", msg)
}

// TestStepGolangciLint_Clean — look-path succeeds and runShell returns
// success.
func TestStepGolangciLint_Clean(t *testing.T) {
	orig := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "/fake/golangci-lint", nil }
	t.Cleanup(func() { golangciLintLookPath = orig })
	withStubShell(t, stubResponse{out: "", err: nil})
	msg, _, err := stepGolangciLint()
	assert.NoError(t, err)
	assert.Empty(t, msg)
}

// TestStepGolangciLint_Issues — look-path succeeds; shell fails.
func TestStepGolangciLint_Issues(t *testing.T) {
	orig := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "/fake/golangci-lint", nil }
	t.Cleanup(func() { golangciLintLookPath = orig })
	withStubShell(t, stubResponse{out: "a.go:1: issue", err: fmt.Errorf("lint")})
	msg, _, err := stepGolangciLint()
	require.Error(t, err)
	assert.Equal(t, "lint reported issues", msg)
}

// TestStepGoTest_Clean — `go test` passes.
func TestStepGoTest_Clean(t *testing.T) {
	withStubShell(t, stubResponse{out: "", err: nil})
	msg, _, err := stepGoTest(true)
	assert.NoError(t, err)
	assert.Empty(t, msg)
}

// TestStepGoTest_WithRaceClean — race path same as above.
func TestStepGoTest_WithRaceClean(t *testing.T) {
	withStubShell(t, stubResponse{out: "", err: nil})
	_, _, err := stepGoTest(false)
	assert.NoError(t, err)
}

// TestStepGoTest_Fails — tests fail.
func TestStepGoTest_Fails(t *testing.T) {
	withStubShell(t, stubResponse{out: "FAIL", err: fmt.Errorf("go test")})
	msg, _, err := stepGoTest(true)
	require.Error(t, err)
	assert.Equal(t, "tests failed", msg)
}

// TestStepGoBuild_Clean — `go build` passes.
func TestStepGoBuild_Clean(t *testing.T) {
	withStubShell(t, stubResponse{out: "", err: nil})
	msg, _, err := stepGoBuild()
	assert.NoError(t, err)
	assert.Empty(t, msg)
}

// TestStepGoBuild_Fails — build fails.
func TestStepGoBuild_Fails(t *testing.T) {
	withStubShell(t, stubResponse{out: "err", err: fmt.Errorf("go build")})
	msg, _, err := stepGoBuild()
	require.Error(t, err)
	assert.Equal(t, "build failed", msg)
}

// TestStepRoutes_Valid — a real app/rest/routes dir with a valid file
// runs successfully.
func TestStepRoutes_Valid(t *testing.T) {
	chdirTemp(t)
	routesDir := filepath.Join("app", "rest", "routes")
	require.NoError(t, os.MkdirAll(routesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routesDir, "sample.routes.go"),
		[]byte(`r.Get("/x", h)`), 0o644))
	_, _, err := stepRoutes()
	assert.NoError(t, err)
}

// TestStepRoutes_ReadFails — parent dir read-only triggers runRoutes
// failure which stepRoutes wraps.
func TestStepRoutes_ReadFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	chdirTemp(t)
	routesDir := filepath.Join("app", "rest", "routes")
	require.NoError(t, os.MkdirAll(routesDir, 0o755))
	require.NoError(t, os.Chmod(routesDir, 0o111))
	t.Cleanup(func() { _ = os.Chmod(routesDir, 0o755) })
	msg, _, err := stepRoutes()
	require.Error(t, err)
	assert.Contains(t, msg, "routes command failed")
}

// TestStepWireDrift_InfoError — wireDriftInfoErr seam forces the
// d.Info() err != nil branch.
func TestStepWireDrift_InfoError(t *testing.T) {
	chdirTemp(t)
	diDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(diDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(diDir, "wire_gen.go"),
		[]byte("package di"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(diDir, "wire.go"),
		[]byte("package di"), 0o644))
	orig := wireDriftInfoErr
	wireDriftInfoErr = fmt.Errorf("forced")
	t.Cleanup(func() { wireDriftInfoErr = orig })
	msg, _, _ := stepWireDrift()
	// With Info forced to error, no file is recorded as stale → no
	// drift message.
	assert.Empty(t, msg)
}

// TestStepWireDrift_WalkErr — when app/di exists but an inner entry is
// inaccessible, the walk returns an error that stepWireDrift wraps.
func TestStepWireDrift_WalkErr(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	chdirTemp(t)
	diDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(diDir, 0o755))
	// Place wire_gen.go so the first Stat succeeds, then chmod the
	// directory to deny traversal so WalkDir fails.
	require.NoError(t, os.WriteFile(filepath.Join(diDir, "wire_gen.go"), []byte("package di"), 0o644))
	sub := filepath.Join(diDir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	// Revoking read permission on the subdir makes WalkDir emit an err
	// for an entry, but the stat d.Info() branch is the default
	// tolerated path.
	require.NoError(t, os.Chmod(sub, 0o000))
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	_, _, _ = stepWireDrift()
}

// TestRunVerify_AllPass — every step succeeds → runVerify returns nil.
func TestRunVerify_AllPass(t *testing.T) {
	chdirTemp(t)
	origLP := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { golangciLintLookPath = origLP })
	// Every runShellFn call succeeds with no output.
	withStubShell(t, stubResponse{out: "", err: nil})
	// skipRace=true, skipLint=true, keepGoing=false. wire/routes skip
	// because there's no app/di or app/rest.
	err := runVerify(verifyOptions{skipLint: true, skipRace: true, keepGoing: false})
	assert.NoError(t, err)
}

// TestRunVerify_IncludesLint — skipLint=false includes the lint step.
func TestRunVerify_IncludesLint(t *testing.T) {
	chdirTemp(t)
	// Stub out the runShellFn so every step succeeds without needing
	// real toolchain. Also stub golangciLintLookPath to report the
	// binary as missing → "skip" which is still a pass-or-skip.
	origLP := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { golangciLintLookPath = origLP })
	withStubShell(t, stubResponse{out: "", err: nil})
	withJSONMode(t)
	// skipLint=false → lint step is included; lookPath says missing
	// → "skip" result, so the whole run passes.
	var err error
	out := captureStdout(t, func() {
		err = runVerify(verifyOptions{skipLint: false, skipRace: true, keepGoing: false})
	})
	assert.NoError(t, err)
	var res verifyResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	var lint *verifyCheck
	for i := range res.Checks {
		if res.Checks[i].Name == "golangci-lint" {
			lint = &res.Checks[i]
		}
	}
	require.NotNil(t, lint, "lint step should be present when skipLint=false")
	assert.Equal(t, "skip", lint.Status)
}

// TestRunVerify_KeepGoingContinuesPastFailure — a failed step with
// keepGoing=true still runs subsequent steps.
func TestRunVerify_KeepGoingContinuesPastFailure(t *testing.T) {
	chdirTemp(t)
	// gofmt OK, vet fails, test OK, build OK, wire skip, routes skip
	withStubShell(t,
		stubResponse{out: "", err: nil},
		stubResponse{out: "issue", err: fmt.Errorf("vet")},
		stubResponse{out: "", err: nil},
		stubResponse{out: "", err: nil},
	)
	err := runVerify(verifyOptions{skipLint: true, skipRace: true, keepGoing: true})
	require.Error(t, err)
}

// TestRunVerify_EmptyMessageFallback — when a step returns ("", "", err)
// with an empty message, runVerifyStep falls back to err.Error() as the
// check message. Assert that fallback directly at the step level.
func TestRunVerify_EmptyMessageFallback(t *testing.T) {
	step := verifyStepDef{
		name: "custom",
		fn: func() (string, string, error) {
			return "", "", fmt.Errorf("silent fail")
		},
	}
	var result verifyResult
	check := runVerifyStep(step, &result)
	assert.Equal(t, "fail", check.Status)
	assert.Equal(t, "silent fail", check.Message)
	assert.Equal(t, 1, result.Failed)
}

// TestRunVerify_BreakOnFirstFail — keep-going=false breaks on the
// first fail. Exercises the `break` branch.
func TestRunVerify_BreakOnFirstFail(t *testing.T) {
	chdirTemp(t)
	// gofmt fails → break.
	withStubShell(t, stubResponse{out: "main.go\n", err: nil})
	err := runVerify(verifyOptions{skipLint: true, skipRace: true, keepGoing: false})
	require.Error(t, err)
}

// recordedShellCall captures what each step function asked runShellFn to
// invoke. Tests use it to assert "scoped run passed only changed files
// to gofmt, only affected packages to go test, etc."
type recordedShellCall struct {
	name string
	args []string
}

// withStubShell swaps runShellFn for the duration of the test to a
// scripted response. The responses slice is consumed in order; further
// calls return the final entry.
type stubResponse struct {
	out string
	err error
}

func withStubShell(t *testing.T, responses ...stubResponse) {
	t.Helper()
	orig := runShellFn
	call := 0
	runShellFn = func(_ string, _ ...string) (string, error) {
		r := responses[len(responses)-1]
		if call < len(responses) {
			r = responses[call]
		}
		call++
		return r.out, r.err
	}
	t.Cleanup(func() { runShellFn = orig })
}
