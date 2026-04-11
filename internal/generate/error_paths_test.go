package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the error branches of the filesystem-touching
// generators. The pattern is uniform: set up the project, make the target
// directory's parent a regular file (so os.MkdirAll fails), call the
// generator, and assert a non-nil error. The generators all use the
// "mkdir then write" sequence, so sabotaging either step surfaces the
// corresponding error path.

// makeParentAFile replaces the given path with a regular file so that any
// subsequent MkdirAll on it returns an error. Parent directories are
// created first so only the leaf component is a file.
func makeParentAFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("not a dir"), 0o644))
}

// --- WriteTemplate filesystem errors ---

func TestWriteTemplate_MkdirAllError(t *testing.T) {
	setupTempProject(t)
	// Make "output" a regular file so MkdirAll("output") inside
	// WriteTemplate fails with ENOTDIR.
	makeParentAFile(t, "output")
	err := WriteTemplate("output/foo.go", "x", "package foo", sampleScaffoldData())
	assert.Error(t, err)
}

func TestWriteTemplate_CreateError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based write denial")
	}
	setupTempProject(t)
	// Create "output" as a read+execute-only directory. MkdirAll("output")
	// sees it already exists and returns nil, then os.Create("output/foo.go")
	// fails with EACCES.
	require.NoError(t, os.Mkdir("output", 0o555))
	t.Cleanup(func() { _ = os.Chmod("output", 0o755) })
	err := WriteTemplate("output/foo.go", "x", "package foo", sampleScaffoldData())
	assert.Error(t, err)
}

// --- GenJob / GenTask / GenEmailTemplate MkdirAll errors ---

func TestGenJob_MkdirAllError(t *testing.T) {
	setupTempProject(t)
	makeParentAFile(t, "app/jobs")
	err := GenJob(sampleScaffoldData())
	assert.Error(t, err)
}

func TestGenTask_MkdirAllError(t *testing.T) {
	setupTempProject(t)
	makeParentAFile(t, "app/tasks")
	err := GenTask(sampleScaffoldData())
	assert.Error(t, err)
}

func TestGenEmailTemplate_MkdirAllError(t *testing.T) {
	setupTempProject(t)
	makeParentAFile(t, "templates/emails")
	err := GenEmailTemplate(sampleScaffoldData())
	assert.Error(t, err)
}

// --- WriteFile error paths for GenJob / GenTask / GenEmailTemplate ---
//
// After MkdirAll succeeds, the final WriteFile fails when the target path
// is itself a directory. We create the target as a dir before calling the
// generator — Stat() at the top of each generator returns err == nil
// (directory exists), so the "skip (exists)" branch fires instead.
//
// To actually reach the WriteFile error we need Stat() to fail but
// WriteFile to fail too. The trick: create a read-only parent directory.
// That's platform-specific (POSIX; not on Windows), and on macOS under
// the test runner it's reliable.

// mkReadOnlyLeaf creates parent dirs at 0o755 then the leaf at 0o555 so the
// leaf exists (MkdirAll is a no-op in the generator) but writes inside fail.
func mkReadOnlyLeaf(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Mkdir(path, 0o555))
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

func TestGenJob_WriteFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based write denial")
	}
	setupTempProject(t)
	mkReadOnlyLeaf(t, "app/jobs")
	err := GenJob(sampleScaffoldData())
	assert.Error(t, err)
}

func TestGenTask_WriteFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based write denial")
	}
	setupTempProject(t)
	mkReadOnlyLeaf(t, "app/tasks")
	err := GenTask(sampleScaffoldData())
	assert.Error(t, err)
}

func TestGenEmailTemplate_WriteFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based write denial")
	}
	setupTempProject(t)
	mkReadOnlyLeaf(t, "templates/emails")
	err := GenEmailTemplate(sampleScaffoldData())
	assert.Error(t, err)
}

// --- GenMigration delegates to WriteTemplate; covering it picks up the
// error path through the generator shell. ---

func TestGenMigration_MkdirError(t *testing.T) {
	setupTempProject(t)
	// db/migrations is created by setupTempProject. Replace it with a
	// regular file so the .up.sql WriteTemplate call fails.
	require.NoError(t, os.RemoveAll("db/migrations"))
	require.NoError(t, os.WriteFile("db/migrations", []byte("x"), 0o644))
	err := GenMigration(sampleScaffoldData())
	assert.Error(t, err)
}

// --- Patcher read errors ---
//
// Each patcher starts with os.ReadFile on a fixed project path. If that
// file is missing we hit the early error return. These tests delete or
// simply don't create the target file before calling the patcher.

func TestPatchContainer_ReadError(t *testing.T) {
	setupTempProject(t)
	// No app/di/container.go — ReadFile returns an error immediately.
	err := PatchContainer(sampleScaffoldData())
	assert.Error(t, err)
}

func TestPatchWireFile_ReadError(t *testing.T) {
	setupTempProject(t)
	err := PatchWireFile(sampleScaffoldData())
	assert.Error(t, err)
}

func TestPatchResolver_ReadError(t *testing.T) {
	setupTempProject(t)
	err := PatchResolver(sampleScaffoldData())
	assert.Error(t, err)
}

func TestPatchRouteConfig_ReadError(t *testing.T) {
	setupTempProject(t)
	err := PatchRouteConfig(sampleScaffoldData())
	assert.Error(t, err)
}

func TestPatchServeFile_ReadError(t *testing.T) {
	setupTempProject(t)
	err := PatchServeFile(sampleScaffoldData())
	assert.Error(t, err)
}

func TestPatchJobRegistry_ReadError(t *testing.T) {
	setupTempProject(t)
	// No cmd/serve.go
	err := PatchJobRegistry(sampleScaffoldData())
	assert.Error(t, err)
}

func TestPatchJobConfig_ReadError(t *testing.T) {
	setupTempProject(t)
	// setupTempProject writes a config.yaml — remove it.
	require.NoError(t, os.Remove("config.yaml"))
	err := PatchJobConfig(sampleScaffoldData())
	assert.Error(t, err)
}

func TestPatchJobConfig_SkipsAlreadyInConfig(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	// Seed config.yaml with the exact active entry for this job name so
	// the "already in config" skip branch fires.
	writeTestFile(t, "config.yaml", "jobs:\n  - name: product\n    schedule: \"0 0 * * * *\"\n")
	err := PatchJobConfig(d)
	require.NoError(t, err)
	// The skip branch does not modify the file.
	content := readTestFile(t, "config.yaml")
	assert.Contains(t, content, "- name: product")
}
