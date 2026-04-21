package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Edge-case coverage for manifest.go — Save, RecordInstall, and
// LoadManifest branches not reached by the happy-path suite.
// ─────────────────────────────────────────────────────────────────────

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
	m := &Manifest{Version: 1, Installed: map[string]InstallRecord{}}
	m.RecordInstall("claude", "v1.0.0")
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
// on a Manifest with a nil Installed map still works.
func TestManifest_RecordInstall_InitializesMap(t *testing.T) {
	m := &Manifest{Installed: nil}
	m.RecordInstall("cursor", "v2.0.0")
	assert.Len(t, m.Installed, 1)
	assert.Equal(t, "v2.0.0", m.Installed["cursor"].CLIVersion)
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
	m := &Manifest{Version: 1}
	err := m.Save(dir)
	require.Error(t, err)
}
