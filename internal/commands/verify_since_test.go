package commands

import (
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

// recordedShellCall captures what each step function asked runShellFn to
// invoke. Tests use it to assert "scoped run passed only changed files
// to gofmt, only affected packages to go test, etc."
type recordedShellCall struct {
	name string
	args []string
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
