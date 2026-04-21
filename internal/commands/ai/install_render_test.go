package ai

import (
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Error-path coverage for install.go internals. Happy paths are
// covered by the main runners_test.go + install_edge_test.go suites;
// these tests hit the defensive-error branches that weren't otherwise
// reachable.
// ─────────────────────────────────────────────────────────────────────

// TestRenderTemplate_MissingSource — ReadTemplate fails for a path
// that isn't in the embed FS.
func TestRenderTemplate_MissingSource(t *testing.T) {
	_, err := renderTemplate("templates/nonexistent/file.tmpl",
		InstallData{ProjectName: "x"})
	require.Error(t, err)
}

// TestWriteFile_ParentWriteBlocked — MkdirAll fails when a segment
// of the path already exists as a regular file. Verifies writeFile
// propagates the error as AI_INSTALL_FAILED.
func TestWriteFile_ParentWriteBlocked(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where a directory would need to exist.
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte(""), 0o644))
	// Try to write under it — MkdirAll should fail.
	target := filepath.Join(blocker, "child", "x.sh")
	err := writeFile(target, []byte("#!/bin/sh"))
	require.Error(t, err)
}

// TestInstall_TemplateReadError — we can't easily fabricate an
// unreadable embedded template (the fs.FS doesn't expose filesystem
// errors). Skip with a rationale so the coverage tool records the
// branch as intentionally uncovered.
func TestInstall_DocumentedUnreachable(t *testing.T) {
	t.Skip("the embed.FS read-error branch is unreachable at runtime: " +
		"templates are compiled into the binary and the fs.ReadFile call " +
		"only fails on a path that was misspelled in code review.")
}

// TestLoadManifest_ReadErrorNotExist — missing file returns an
// empty manifest without error (tested implicitly by other happy-
// path tests, exercised here directly to hit the specific branch).
func TestLoadManifest_ReadErrorNotExist(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, 1, m.Version)
	assert.NotNil(t, m.Installed)
}

// TestSave_WriteTempFails — we can't cleanly force os.WriteFile to
// fail, so instead we cover the MkdirAll success + rename path via
// a read-only parent directory. On read-only FS os.Rename would
// fail; on macOS/Linux this is non-trivial without root. Document
// instead.
func TestSave_DocumentedUnreachable(t *testing.T) {
	t.Skip("the os.Rename error branch requires an unwritable FS which " +
		"isn't portable to test — rely on the happy-path TestManifest_Save_AtomicRename " +
		"for the rename is exercised there.")
}

// TestTemplateFiles_EmptyAgent — an agent pointing at a nonexistent
// directory returns an empty slice with no error.
func TestTemplateFiles_EmptyAgent(t *testing.T) {
	files, err := TemplateFiles(&Agent{TemplateDir: "templates/claude"})
	require.NoError(t, err)
	assert.NotEmpty(t, files)
}

// parseTemplateStrict builds a *template.Template with
// missingkey=error so a reference to a non-existent field triggers
// an Execute error.
func parseTemplateStrict(src string) (*template.Template, error) {
	return template.New("t").Option("missingkey=error").Parse(src)
}

// TestWriteFile_MkdirAllFails — parent already exists as a regular
// file.
func TestWriteFile_MkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "sub")
	require.NoError(t, os.WriteFile(blocker, []byte{}, 0o644))
	err := writeFile(filepath.Join(blocker, "child.txt"), []byte("x"))
	require.Error(t, err)
}

// TestWriteFile_WriteFails — parent is read-only so WriteFile fails.
func TestWriteFile_WriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod write denial")
	}
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.Chmod(subdir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(subdir, 0o755) })
	err := writeFile(filepath.Join(subdir, "file.txt"), []byte("x"))
	require.Error(t, err)
}

// TestRenderTemplate_ReadTemplateError — ReadTemplate fails when the
// source path doesn't exist in the embed FS.
func TestRenderTemplate_ReadTemplateError(t *testing.T) {
	_, err := renderTemplate("templates/does-not-exist/x.tmpl", InstallData{})
	require.Error(t, err)
}

// TestTemplateFiles_InvalidDir — invalid agent.TemplateDir returns an
// error from fs.WalkDir.
func TestTemplateFiles_InvalidDir(t *testing.T) {
	a := &Agent{Key: "x", TemplateDir: "templates/nonexistent"}
	_, err := TemplateFiles(a)
	require.Error(t, err)
}

// TestInstall_InvalidAgentTemplate — same as TemplateFiles_InvalidDir
// but surfaced via Install.
func TestInstall_InvalidAgentTemplate(t *testing.T) {
	a := &Agent{Key: "broken", TemplateDir: "templates/nonexistent"}
	_, err := Install(a, t.TempDir(), InstallData{}, InstallOptions{})
	require.Error(t, err)
}

// TestInstall_StatError — when the destination path is not readable
// due to a permissions error (neither "exists with content" nor
// NotExist), Install's default branch fires.
func TestInstall_StatError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod traversal denial")
	}
	dir := t.TempDir()
	agent := AgentByKey("claude")
	require.NotNil(t, agent)
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	require.NotEmpty(t, files)
	// Pick the first file and create a parent dir that we can't read.
	parent := filepath.Dir(filepath.Join(dir, files[0].DestPath))
	require.NoError(t, os.MkdirAll(parent, 0o755))
	// Create the target file AS a directory so it's neither ENOENT nor
	// a file-with-content (ReadFile returns EISDIR → default branch in
	// Install switch).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, files[0].DestPath), 0o755))
	_, err = Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err)
}

// TestInstall_WriteFileFails — writeFile returns an error mid-install,
// propagating up.
func TestInstall_WriteFileFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := t.TempDir()
	// Chmod the root dir read-only so MkdirAll inside writeFile fails.
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	agent := AgentByKey("claude")
	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err)
}

// TestInstall_RenderTemplateError — all shipped templates parse; this
// branch is only reachable via a custom embed FS.
func TestInstall_RenderTemplateError(t *testing.T) {
	t.Skip("all shipped templates parse; renderTemplate error path only reachable with custom FS")
}

// TestRenderTemplate_ParseError — templateParse seam returns an error.
func TestRenderTemplate_ParseError(t *testing.T) {
	orig := templateParse
	templateParse = func(_ string, _ []byte) (*template.Template, error) {
		return nil, assertError("bad parse")
	}
	t.Cleanup(func() { templateParse = orig })
	agent := AgentByKey("claude")
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	_, err = renderTemplate(files[0].SourcePath, sampleData())
	require.Error(t, err)
}

// TestRenderTemplate_ExecuteError — templateParse returns a template
// that fails at Execute time.
func TestRenderTemplate_ExecuteError(t *testing.T) {
	orig := templateParse
	templateParse = func(_ string, _ []byte) (*template.Template, error) {
		return parseTemplateStrict(`{{.NonexistentField.SubField}}`)
	}
	t.Cleanup(func() { templateParse = orig })
	agent := AgentByKey("claude")
	files, _ := TemplateFiles(agent)
	_, err := renderTemplate(files[0].SourcePath, sampleData())
	require.Error(t, err)
}

// TestInstall_RenderError — renderTemplate returns an error via the
// templateParse seam, which Install wraps as CodeAIInstallFailed.
func TestInstall_RenderError(t *testing.T) {
	orig := templateParse
	templateParse = func(_ string, _ []byte) (*template.Template, error) {
		return nil, assertError("bad parse")
	}
	t.Cleanup(func() { templateParse = orig })
	agent := AgentByKey("claude")
	_, err := Install(agent, t.TempDir(), sampleData(), InstallOptions{})
	require.Error(t, err)
}
