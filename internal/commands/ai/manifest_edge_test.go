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
