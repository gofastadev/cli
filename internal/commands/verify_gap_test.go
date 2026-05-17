package commands

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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

// — resolveVerifyScopeImpl: walk every branch using real git repos.

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
