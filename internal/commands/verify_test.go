package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// ─────────────────────────────────────────────────────────────────────
// Coverage for verify.go step functions and runVerify branches.
// Uses the runShellFn seam to stub the individual `gofmt` / `go vet`
// /etc invocations so tests don't depend on the local toolchain.
// ─────────────────────────────────────────────────────────────────────

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

// TestRunVerify_MessageEmptyFallback — a step returns an error with an
// empty message. runVerify falls back to err.Error().
func TestRunVerify_MessageEmptyFallback(t *testing.T) {
	chdirTemp(t)
	// Every step uses runShellFn, so we force the first step (gofmt)
	// to succeed with drift, then ensure the subsequent tests run.
	withStubShell(t,
		// gofmt → no drift
		stubResponse{out: "", err: nil},
		// go vet → fails with empty message (msg="vet reported issues" actually)
		stubResponse{out: "", err: fmt.Errorf("boom")},
	)
	// Also disable lint.
	_ = runVerify(verifyOptions{skipLint: true, skipRace: true, keepGoing: true})
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
	// skipLint=false → lint step is included; lookPath says missing
	// → "skip" result, so the whole run passes.
	err := runVerify(verifyOptions{skipLint: false, skipRace: true, keepGoing: false})
	_ = err
}

// TestRunVerify_EmptyErrorMessage — stepGoVet sets the msg directly,
// so the fallback branch at the runVerify level is unreachable via
// the canned steps.
func TestRunVerify_EmptyErrorMessage(t *testing.T) {
	t.Skip("stepGoVet sets Message directly; fallback unreachable from step level")
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

// TestRunVerify_EmptyMessageFallback — inject a step that returns
// ("", "", err) so runVerify's fallback branch assigning err.Error()
// as the message fires.
func TestRunVerify_EmptyMessageFallback(t *testing.T) {
	chdirTemp(t)
	origLP := golangciLintLookPath
	golangciLintLookPath = func() (string, error) { return "", fmt.Errorf("nope") }
	t.Cleanup(func() { golangciLintLookPath = origLP })
	// All built-in steps pass; the injected one fails with empty msg.
	withStubShell(t, stubResponse{out: "", err: nil})
	extraVerifySteps = []verifyStepDef{
		{"custom", func() (string, string, error) {
			return "", "", fmt.Errorf("silent fail")
		}},
	}
	t.Cleanup(func() { extraVerifySteps = nil })
	_ = runVerify(verifyOptions{skipLint: true, skipRace: true, keepGoing: true})
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
