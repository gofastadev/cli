package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestCheckPendingMigrations_Counts — migrations present → warn status
// with a count.
func TestCheckPendingMigrations_Counts(t *testing.T) {
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
	assert.Equal(t, "warn", check.Status)
	assert.Contains(t, check.Message, "2 migration(s)")
}

func TestCheckPendingMigrations_NoDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	check := checkPendingMigrations()
	assert.Equal(t, "skip", check.Status)
}
