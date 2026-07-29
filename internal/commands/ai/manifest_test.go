package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManifest_LoadSaveRoundtrip — manifest round-trips cleanly through
// disk and InstallRecord data survives intact.
func TestManifest_LoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Empty(t, m.Installed, "fresh manifest should be empty")
	assert.Equal(t, manifestSchemaVersion, m.Version)

	m.RecordInstall("claude", "v0.5.0-test",
		[]string{".claude/settings.json", ".claude/commands/verify.md"})
	require.NoError(t, m.Save(dir))

	m2, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "claude", m2.ActiveAgent)
	rec, ok := m2.Installed["claude"]
	require.True(t, ok)
	assert.Equal(t, "v0.5.0-test", rec.CLIVersion)
	assert.Equal(t, []string{".claude/settings.json", ".claude/commands/verify.md"}, rec.CreatedFiles)
}

// TestLoadManifest_ReadErrorNotExist — missing file returns an
// empty manifest without error (tested implicitly by other happy-
// path tests, exercised here directly to hit the specific branch).
func TestLoadManifest_ReadErrorNotExist(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, manifestSchemaVersion, m.Version)
	assert.NotNil(t, m.Installed)
}

// TestLoadManifest_MalformedJSON — existing file with broken JSON
// surfaces AI_MANIFEST_IO.
func TestLoadManifest_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, manifestPath), []byte("{not-json"), 0o644))
	_, err := LoadManifest(dir)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "AI_MANIFEST_IO")
}

// TestLoadManifest_NilInstalledDefaulted — reading a manifest written
// without an `installed` field still yields a non-nil map so
// downstream RecordInstall doesn't need to check for nil.
func TestLoadManifest_NilInstalledDefaulted(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, manifestPath), []byte(`{"version":1}`), 0o644))
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	require.NotNil(t, m.Installed)
	assert.Empty(t, m.Installed)
}

// TestManifest_Save_AtomicRename — Save writes the manifest in-place
// via a temp file + rename. A successful Save leaves exactly one
// file, not a leftover .tmp.
func TestManifest_Save_AtomicRename(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{Version: manifestSchemaVersion, Installed: map[string]InstallRecord{}}
	m.RecordInstall("claude", "v1.0.0", nil)
	require.NoError(t, m.Save(dir))

	// Main file exists.
	_, err := os.Stat(filepath.Join(dir, manifestPath))
	require.NoError(t, err)
	// Temp file doesn't linger.
	_, err = os.Stat(filepath.Join(dir, manifestPath+".tmp"))
	assert.True(t, os.IsNotExist(err), "leftover .tmp file after Save")
}

// TestManifest_Save_CantCreateDir — parent write permission denied.
// Simulated by passing a path that already exists as a regular file.
func TestManifest_Save_CantCreateDir(t *testing.T) {
	dir := t.TempDir()
	// .gofasta exists as a FILE, not a dir — MkdirAll will fail.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gofasta"), []byte{}, 0o644))
	m := &Manifest{Version: 1, Installed: map[string]InstallRecord{}}
	err := m.Save(dir)
	require.Error(t, err)
}

// TestManifest_RecordInstall_InitializesMap — calling RecordInstall
// on a Manifest with a nil Installed map still works, and ActiveAgent
// is set as a side-effect.
func TestManifest_RecordInstall_InitializesMap(t *testing.T) {
	m := &Manifest{Installed: nil}
	m.RecordInstall("cursor", "v2.0.0", nil)
	assert.Len(t, m.Installed, 1)
	assert.Equal(t, "v2.0.0", m.Installed["cursor"].CLIVersion)
	assert.Equal(t, "cursor", m.ActiveAgent)
}

// TestLoadManifest_ReadFileError — file exists but can't be read.
func TestLoadManifest_ReadFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	path := filepath.Join(dir, manifestPath)
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	_, err := LoadManifest(dir)
	require.Error(t, err)
}

// TestManifest_Save_RenameFails — tmp file writes ok but Rename fails
// because the target path already exists as a directory.
func TestManifest_Save_RenameFails(t *testing.T) {
	dir := t.TempDir()
	// .gofasta dir exists, and we put a SUBDIR at the manifest path so
	// Rename attempting to overwrite it fails.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, manifestPath), 0o755))
	m := &Manifest{Version: 1, Installed: map[string]InstallRecord{}}
	err := m.Save(dir)
	require.Error(t, err)
}

// TestManifest_Save_MarshalError — forces the json.MarshalIndent error
// branch via the manifestMarshal seam.
func TestManifest_Save_MarshalError(t *testing.T) {
	orig := manifestMarshal
	manifestMarshal = func(_ any, _, _ string) ([]byte, error) {
		return nil, assertError("marshal boom")
	}
	t.Cleanup(func() { manifestMarshal = orig })
	dir := t.TempDir()
	m := &Manifest{Version: manifestSchemaVersion}
	err := m.Save(dir)
	require.Error(t, err)
}

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

// TestLoadManifest_MigratesV1ToV2 — write a v1 manifest with one
// installed agent, load it, assert ActiveAgent is inferred and
// Version was bumped.
func TestLoadManifest_MigratesV1ToV2(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	v1 := `{
		"version": 1,
		"installed": {
			"claude": {
				"installed_at": "2026-01-01T00:00:00Z",
				"cli_version": "v0.1.0"
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestPath),
		[]byte(v1), 0o644))

	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "claude", m.ActiveAgent, "v1→v2 should infer single installed agent as active")
	assert.Equal(t, manifestSchemaVersion, m.Version)
}

// TestLoadManifest_V1MultipleInstalledLeavesActiveEmpty — if v1 had
// more than one installed entry (legacy quirk, the v1 schema technically
// allowed it), ActiveAgent stays empty. The next install will populate.
func TestLoadManifest_V1MultipleInstalledLeavesActiveEmpty(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	v1 := `{
		"version": 1,
		"installed": {
			"claude": {"installed_at": "2026-01-01T00:00:00Z", "cli_version": "v0.1.0"},
			"cursor": {"installed_at": "2026-01-02T00:00:00Z", "cli_version": "v0.1.0"}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestPath),
		[]byte(v1), 0o644))

	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Empty(t, m.ActiveAgent, "ambiguous v1 manifest should not infer active agent")
	assert.Equal(t, manifestSchemaVersion, m.Version)
}

// TestRunUninstall_LoadManifestError — corrupt manifest causes
// LoadManifest to fail; runUninstall surfaces the error.
func TestRunUninstall_LoadManifestError(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestPath),
		[]byte("not-json"), 0o644))
	err := runUninstall("claude", false)
	require.Error(t, err)
}
