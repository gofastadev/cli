package ai

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for the ai/ai.go runners (runInstall, runList, runStatus,
// printNextSteps, findProjectRoot, buildInstallData,
// SetVersionResolver) and the InstallResult + manifest helpers
// (PrintText, InstalledKeys).
//
// Most of these chdir to a temp dir containing a minimal go.mod so
// findProjectRoot resolves without touching the user's real
// filesystem.
// ─────────────────────────────────────────────────────────────────────

// scaffoldFakeProject creates a temporary directory that looks like a
// gofasta project to the ai package's helpers — just a go.mod with a
// module declaration is enough. Chdirs into it for the duration of
// the test so the install path is predictable.
func scaffoldFakeProject(t *testing.T, modulePath string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module "+modulePath+"\n\ngo 1.25.0\n"),
		0o644,
	))
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
}

// ── findProjectRoot ──────────────────────────────────────────────────

func TestFindProjectRoot_AtRoot(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	got, err := findProjectRoot()
	require.NoError(t, err)
	// Resolve both paths to handle macOS /var/private symlink quirks.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	assert.Equal(t, wantResolved, gotResolved)
}

// TestFindProjectRoot_WalksUp — starting from a subdirectory still
// finds the go.mod above.
func TestFindProjectRoot_WalksUp(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	sub := filepath.Join(dir, "app", "models")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.Chdir(sub))
	got, err := findProjectRoot()
	require.NoError(t, err)
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	assert.Equal(t, wantResolved, gotResolved)
}

// TestFindProjectRoot_NotInsideModule — no go.mod anywhere →
// CodeNotGofastaProject.
func TestFindProjectRoot_NotInsideModule(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_, err := findProjectRoot()
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "NOT_GOFASTA_PROJECT")
}

// ── buildInstallData ─────────────────────────────────────────────────

func TestBuildInstallData_HappyPath(t *testing.T) {
	dir := scaffoldFakeProject(t, "github.com/acme/myapp")
	data, err := buildInstallData(dir)
	require.NoError(t, err)
	assert.Equal(t, "github.com/acme/myapp", data.ModulePath)
	assert.Equal(t, "myapp", data.ProjectName)
	assert.Equal(t, "myapp", data.ProjectNameLower)
	assert.Equal(t, "MYAPP", data.ProjectNameUpper)
	// Default when no version resolver is registered.
	assert.Equal(t, "dev", data.CLIVersion)
}

func TestBuildInstallData_VersionResolver(t *testing.T) {
	dir := scaffoldFakeProject(t, "github.com/acme/myapp")
	SetVersionResolver(func() string { return "v1.2.3" })
	t.Cleanup(func() { SetVersionResolver(func() string { return "" }) })
	data, err := buildInstallData(dir)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", data.CLIVersion)
}

func TestBuildInstallData_MissingGoMod(t *testing.T) {
	dir := t.TempDir()
	_, err := buildInstallData(dir)
	require.Error(t, err)
}

// TestSetVersionResolver_NilKeepsCurrent — passing nil must not wipe
// the existing resolver (defensive against mistaken init order).
func TestSetVersionResolver_NilKeepsCurrent(t *testing.T) {
	SetVersionResolver(func() string { return "stable" })
	SetVersionResolver(nil)
	t.Cleanup(func() { SetVersionResolver(func() string { return "" }) })
	assert.Equal(t, "stable", rootCmdVersion())
}

// ── runList ──────────────────────────────────────────────────────────

func TestRunList_WritesTable(t *testing.T) {
	// runList emits via cliout.Print → os.Stdout. Verify by swapping
	// stdout to a pipe for the duration of the call.
	out := captureStdout(t, func() {
		require.NoError(t, runList())
	})
	assert.Contains(t, out, "KEY")
	for _, a := range Agents {
		assert.Contains(t, out, a.Key)
	}
}

// ── runStatus ────────────────────────────────────────────────────────

func TestRunStatus_EmptyProject(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	out := captureStdout(t, func() {
		require.NoError(t, runStatus())
	})
	assert.Contains(t, out, "No AI agents installed")
}

func TestRunStatus_WithInstalledManifest(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.RecordInstall("claude", "v1.0.0")
	require.NoError(t, m.Save(dir))

	out := captureStdout(t, func() {
		require.NoError(t, runStatus())
	})
	assert.Contains(t, out, "claude")
	assert.Contains(t, out, "v1.0.0")
}

// ── runInstall ───────────────────────────────────────────────────────

func TestRunInstall_UnknownAgent(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	err := runInstall("nonexistent", false, false)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "UNKNOWN_AGENT")
}

func TestRunInstall_DryRunDoesNotWriteFiles(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	// Capture stdout so the result table doesn't pollute test output.
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", true, false))
	})
	// In dry-run mode the .claude directory should NOT exist.
	_, err := os.Stat(filepath.Join(dir, ".claude"))
	assert.True(t, os.IsNotExist(err), "claude dir should not exist after dry-run")
	// The manifest should also not be updated.
	m, _ := LoadManifest(dir)
	assert.Empty(t, m.Installed)
}

func TestRunInstall_RealRunCreatesFiles(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	// Claude templates render into .claude/.
	_, err := os.Stat(filepath.Join(dir, ".claude"))
	require.NoError(t, err)
	// Manifest recorded the install.
	m, _ := LoadManifest(dir)
	assert.Contains(t, m.Installed, "claude")
}

func TestRunInstall_IdempotentSecondRun(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	// Second run should succeed without --force — every file is
	// byte-identical so Install records them as Skipped.
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	_ = dir
}

// ── PrintText — InstallResult formatting ─────────────────────────────

func TestInstallResult_PrintText_AllSections(t *testing.T) {
	r := &InstallResult{
		Agent:        "claude",
		Created:      []string{"a", "b"},
		Replaced:     []string{"c"},
		WouldReplace: []string{"d"},
		Skipped:      []string{"e"},
	}
	var buf bytes.Buffer
	r.PrintText(&buf)
	out := buf.String()
	assert.Contains(t, out, "created 2 file(s)")
	assert.Contains(t, out, "replaced 1 file(s)")
	assert.Contains(t, out, "would replace 1 file(s)")
	assert.Contains(t, out, "skipped 1 unchanged")
}

func TestInstallResult_PrintText_EmptyResultIsSilent(t *testing.T) {
	var buf bytes.Buffer
	(&InstallResult{Agent: "x"}).PrintText(&buf)
	assert.Empty(t, buf.String())
}

// ── printNextSteps ───────────────────────────────────────────────────

func TestPrintNextSteps_EachAgent(t *testing.T) {
	for _, a := range Agents {
		t.Run(a.Key, func(t *testing.T) {
			var buf bytes.Buffer
			printNextSteps(&buf, &a)
			assert.Contains(t, buf.String(), "Next steps")
		})
	}
}

// ── manifest.InstalledKeys ───────────────────────────────────────────

func TestManifest_InstalledKeys_SortedStable(t *testing.T) {
	m := &Manifest{
		Installed: map[string]InstallRecord{
			"windsurf": {},
			"claude":   {},
			"cursor":   {},
		},
	}
	got := m.InstalledKeys()
	assert.Equal(t, []string{"claude", "cursor", "windsurf"}, got)
}

// captureStdout redirects os.Stdout for the duration of fn and
// returns whatever was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return strings.TrimSpace(<-done)
}

// assertError is a tiny string error used by the seam-based error
// tests that inject custom failures into template parsers and marshal
// calls.
type assertError string

func (e assertError) Error() string { return string(e) }

// TestRunInstall_FindProjectRootError — outside any Go module,
// runInstall returns the error from findProjectRoot without trying
// to install.
func TestRunInstall_FindProjectRootError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	// t.TempDir is under /var which has no go.mod.
	err := runInstall("claude", false, false)
	require.Error(t, err)
}

// TestRunInstall_LoadManifestError — corrupt manifest makes
// LoadManifest fail after Install succeeds.
func TestRunInstall_LoadManifestError(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	// Pre-populate a corrupt manifest file.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestPath),
		[]byte("not-json"), 0o644))
	_ = captureStdout(t, func() {
		err := runInstall("claude", false, false)
		require.Error(t, err)
	})
}

// TestRunInstall_ManifestSaveError — after successful install+load,
// Save fails because .gofasta is read-only.
func TestRunInstall_ManifestSaveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	gofastaDir := filepath.Join(dir, ".gofasta")
	require.NoError(t, os.MkdirAll(gofastaDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(gofastaDir, 0o755) })
	_ = captureStdout(t, func() {
		err := runInstall("claude", false, false)
		require.Error(t, err)
	})
}

// TestRunInstall_BuildInstallDataError — unreadable go.mod causes
// buildInstallData to fail after findProjectRoot succeeded.
func TestRunInstall_BuildInstallDataError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.Chmod(filepath.Join(dir, "go.mod"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "go.mod"), 0o644) })
	err := runInstall("claude", false, false)
	require.Error(t, err)
}

// TestRunStatus_LoadManifestError — corrupt manifest makes runStatus
// fail.
func TestRunStatus_LoadManifestError(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestPath),
		[]byte("not-json"), 0o644))
	err := runStatus()
	require.Error(t, err)
}

// TestRunStatus_FindProjectRootError — runStatus outside a Go module.
func TestRunStatus_FindProjectRootError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := runStatus()
	require.Error(t, err)
}

// TestFindProjectRoot_GetwdError — forces the os.Getwd branch via
// the getwd seam.
func TestFindProjectRoot_GetwdError(t *testing.T) {
	orig := getwd
	getwd = func() (string, error) { return "", assertError("boom") }
	t.Cleanup(func() { getwd = orig })
	_, err := findProjectRoot()
	require.Error(t, err)
}

// TestRunInstall_InstallError — a conflicting destination file with
// differing content triggers Install to return an error, which
// runInstall propagates.
func TestRunInstall_InstallError(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	agent := AgentByKey("claude")
	require.NotNil(t, agent)
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	// Pre-populate the first destination with conflicting bytes.
	dst := filepath.Join(dir, files[0].DestPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("conflict"), 0o644))
	err = runInstall("claude", false, false)
	require.Error(t, err)
}
