package stackresolve

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseFrame_LineOverflow — a huge numeric prefix matches the
// regex but overflows int64; strconv.Atoi errors, covering line 74-77.
func TestParseFrame_LineOverflow(t *testing.T) {
	_, _, _, err := ParseFrame("/a/b.go:99999999999999999999999999 pkg.Func")
	require.Error(t, err)
}

// TestResolve_AbsPathUnderCwd_RewritesToRel — absolute path that's
// actually under cwd should round-trip the rel-path branch. Use the
// *resolved* cwd (post-symlink) so macOS /var → /private/var doesn't
// confuse the under-cwd check.
func TestResolve_AbsPathUnderCwd_RewritesToRel(t *testing.T) {
	origCwd, err := os.Getwd()
	require.NoError(t, err)

	tmp := t.TempDir()
	t.Cleanup(func() { _ = os.Chdir(origCwd) })
	require.NoError(t, os.Chdir(tmp))

	// After chdir, ask for the *resolved* working dir so paths that go
	// through symlinks (e.g. macOS /var → /private/var) match.
	resolvedCwd, err := os.Getwd()
	require.NoError(t, err)

	path := filepath.Join(resolvedCwd, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte("package x\nvar a = 1\n"), 0o644))

	rf, err := Resolve(path+":2 pkg.fn", 0)
	require.NoError(t, err)
	require.False(t, rf.External)
	require.Equal(t, "sample.go", rf.File)
}

// TestReadSourceWindow_NegativeCtxClampedToZero — negative ctx is
// clamped to 0 (line 145-147).
func TestReadSourceWindow_NegativeCtxClampedToZero(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(path, []byte("package x\nvar a = 1\nvar b = 2\n"), 0o644))

	win, err := readSourceWindow(path, 2, -5)
	require.NoError(t, err)
	require.Equal(t, 2, win.Current.Line)
	require.Empty(t, win.Before)
	require.Empty(t, win.After)
}

// TestReadSourceWindow_ScannerError — a file whose lines exceed the
// scanner's 1 MiB buffer triggers sc.Err() and surfaces an error.
func TestReadSourceWindow_ScannerError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "huge.go")
	// One line of 2 MiB exceeds the scanner's max-buffer setting.
	long := strings.Repeat("a", 2*1024*1024)
	require.NoError(t, os.WriteFile(path, []byte(long+"\n"), 0o644))

	_, err := readSourceWindow(path, 1, 0)
	require.Error(t, err)
}

// TestUnderCwd_RelativePath — non-absolute paths always return true.
func TestUnderCwd_RelativePath(t *testing.T) {
	require.True(t, underCwd("relative/path.go"))
}

// TestUnderCwd_OutsideCwd — absolute path outside cwd returns false.
func TestUnderCwd_OutsideCwd(t *testing.T) {
	require.False(t, underCwd("/usr/local/go/src/runtime/proc.go"))
}

// TestUnderCwd_UnderCwd — absolute path under cwd returns true.
func TestUnderCwd_UnderCwd(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.True(t, underCwd(filepath.Join(cwd, "some/file.go")))
}

// TestRelToCwd_RelativeInputReturnedUnchanged — non-absolute paths
// short-circuit and return as-is.
func TestRelToCwd_RelativeInputReturnedUnchanged(t *testing.T) {
	require.Equal(t, "foo/bar.go", relToCwd("foo/bar.go"))
}

// TestRelToCwd_UnderCwd — absolute path under cwd is made relative.
func TestRelToCwd_UnderCwd(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	got := relToCwd(filepath.Join(cwd, "a/b.go"))
	require.Equal(t, "a/b.go", got)
}

// TestRelToCwd_OutsideCwd — path outside cwd yields "" (filepath.Rel
// returns a path starting with "..").
func TestRelToCwd_OutsideCwd(t *testing.T) {
	require.Equal(t, "", relToCwd("/some/totally/unrelated/path.go"))
}

// — Inject seam failures to cover the defensive os.Getwd / filepath.Abs /
// filepath.Rel error branches in underCwd and relToCwd.

func TestUnderCwd_GetwdError(t *testing.T) {
	saved := getwdFn
	getwdFn = func() (string, error) { return "", errStub }
	t.Cleanup(func() { getwdFn = saved })
	require.False(t, underCwd("/abs/path/x.go"))
}

func TestUnderCwd_AbsError(t *testing.T) {
	saved := filepathAbsFn
	filepathAbsFn = func(_ string) (string, error) { return "", errStub }
	t.Cleanup(func() { filepathAbsFn = saved })
	require.False(t, underCwd("/abs/path/x.go"))
}

func TestUnderCwd_RelError(t *testing.T) {
	saved := filepathRelFn
	filepathRelFn = func(_, _ string) (string, error) { return "", errStub }
	t.Cleanup(func() { filepathRelFn = saved })
	require.False(t, underCwd("/abs/path/x.go"))
}

func TestRelToCwd_GetwdError(t *testing.T) {
	saved := getwdFn
	getwdFn = func() (string, error) { return "", errStub }
	t.Cleanup(func() { getwdFn = saved })
	require.Equal(t, "", relToCwd("/abs/path/x.go"))
}

func TestRelToCwd_AbsError(t *testing.T) {
	saved := filepathAbsFn
	filepathAbsFn = func(_ string) (string, error) { return "", errStub }
	t.Cleanup(func() { filepathAbsFn = saved })
	require.Equal(t, "", relToCwd("/abs/path/x.go"))
}

// errStub is a small sentinel used by the seam-failure tests above.
var errStub = ioErr("stub error")

type ioErr string

func (e ioErr) Error() string { return string(e) }

// Force `io` to remain imported even after future test edits.
var _ = io.Discard
