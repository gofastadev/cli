package commands

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for new.go walk-error / template-error branches. Uses the
// projectFSOverride seam to inject synthetic filesystems that trigger
// specific failure modes.
// ─────────────────────────────────────────────────────────────────────

// TestRunNew_ChdirFails — projectDir is created but Chdir fails.
// Simulate by making the created dir unreadable so Chdir returns
// EACCES on the next step. This specifically forces the
// `if err := os.Chdir(projectDir); err != nil { return err }` branch.
func TestRunNew_ChdirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	chdirTemp(t)
	// MkdirAll with mode 0o755 succeeds, but we can replace it after
	// — setting mode 0o000 blocks Chdir.
	origOS := osChdir
	osChdir = func(path string) error { return os.ErrPermission }
	t.Cleanup(func() { osChdir = origOS })
	withFakeExec(t, 0)
	err := runNew("chdir-fail-app", false)
	require.Error(t, err)
}

// TestRunNew_BadTemplate — inject a synthetic FS containing a .tmpl
// file whose body is malformed → template.Parse fails.
func TestRunNew_BadTemplate(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	// Build a minimal FS that WalkDir can traverse. The walk expects
	// "project" at the root.
	fsys := fstest.MapFS{
		"project":             {Mode: fs.ModeDir},
		"project/broken.tmpl": {Data: []byte("{{.MissingClose")},
	}
	projectFSOverride = fsys
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("bad-tmpl-app", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing template")
}

// TestRunNew_TemplateExecFails — template parses but Execute fails.
// Achieved with a template that references a field missing from the
// ProjectData type via missingkey=error semantics… but text/template
// default isn't strict. Use a template that calls a method on a nil
// value instead. {{.MissingField.SubField}} errors on Execute.
func TestRunNew_TemplateExecFails(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	fsys := fstest.MapFS{
		"project":             {Mode: fs.ModeDir},
		"project/bad.go.tmpl": {Data: []byte("{{.NoSuchField.Sub}}")},
	}
	projectFSOverride = fsys
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("bad-exec-app", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executing template")
}

// errFS is a small fs.FS implementation that returns an error on
// ReadFile for a specific path but lets WalkDir pass.
type errFS struct{ base fs.FS }

func (e errFS) Open(name string) (fs.File, error)    { return e.base.Open(name) }
func (e errFS) ReadFile(name string) ([]byte, error) { return nil, fs.ErrPermission }
func (e errFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if rd, ok := e.base.(fs.ReadDirFS); ok {
		return rd.ReadDir(name)
	}
	return nil, fs.ErrInvalid
}

// TestRunNew_ReadFileFails — fs.ReadFile returns an error for a
// specific file during the walk.
func TestRunNew_ReadFileFails(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	base := fstest.MapFS{
		"project":       {Mode: fs.ModeDir},
		"project/a.txt": {Data: []byte("x")},
	}
	projectFSOverride = errFS{base: base}
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("read-fail-app", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading")
}

// TestRunNew_WalkCallbackReceivesError — root "project" doesn't exist
// in our override FS → WalkDir's callback is invoked with an err
// describing the missing path, covering the err-param branch.
func TestRunNew_WalkCallbackReceivesError(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	// Empty FS — "project" does not exist → WalkDir invokes callback
	// with an fs.PathError → the first branch in the callback fires.
	projectFSOverride = fstest.MapFS{}
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("walkerr-app", false)
	require.Error(t, err)
}

// satisfy path/filepath import if nothing else pulls it — keep for
// future edge-case tests.
var _ = filepath.Separator
