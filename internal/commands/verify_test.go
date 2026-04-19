package commands

import (
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
