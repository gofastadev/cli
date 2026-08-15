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

// Force `io` to remain imported even after future test edits.
var _ = io.Discard

func TestParseFrame_HappyPath(t *testing.T) {
	file, line, fn, err := ParseFrame("/abs/path/to/file.go:42 pkg.Func")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file != "/abs/path/to/file.go" || line != 42 || fn != "pkg.Func" {
		t.Errorf("got (%q, %d, %q)", file, line, fn)
	}
}

func TestParseFrame_MethodReceiverFormat(t *testing.T) {
	_, _, fn, err := ParseFrame("/a/b.go:1 irodata/app/services.(*orderService).Archive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fn != "irodata/app/services.(*orderService).Archive" {
		t.Errorf("Func = %q", fn)
	}
}

func TestParseFrame_RelativePath(t *testing.T) {
	file, _, _, err := ParseFrame("app/services/order.service.go:10 pkg.Func")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file != "app/services/order.service.go" {
		t.Errorf("file = %q", file)
	}
}

func TestParseFrame_Malformed(t *testing.T) {
	cases := []string{
		"",
		"no colon here",
		"file.go:abc pkg.Func",
		"file.go pkg.Func",
		":42 pkg.Func", // empty file accepted by greedy regex but the leading "" fails — actually no, regex won't match
	}
	for _, c := range cases {
		_, _, _, err := ParseFrame(c)
		if err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestResolve_ReadsSourceWindow(t *testing.T) {
	// Create a temp file we can frame against.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sample.go")
	src := `package x

func f() {
	x := 1
	y := 2
	z := x + y
	_ = z
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	// Frame points at line 6 ("z := x + y") with ±1 context.
	raw := path + ":6 sample.f"
	rf, err := Resolve(raw, 1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rf.Source == nil {
		t.Fatal("Source is nil — expected window")
	}
	if rf.Source.Current.Line != 6 {
		t.Errorf("Current.Line = %d, want 6", rf.Source.Current.Line)
	}
	if !strings.Contains(rf.Source.Current.Text, "z := x + y") {
		t.Errorf("Current.Text = %q", rf.Source.Current.Text)
	}
	if len(rf.Source.Before) != 1 || rf.Source.Before[0].Line != 5 {
		t.Errorf("Before = %#v", rf.Source.Before)
	}
	if len(rf.Source.After) != 1 || rf.Source.After[0].Line != 7 {
		t.Errorf("After = %#v", rf.Source.After)
	}
}

func TestResolve_MissingFileMarkedExternal(t *testing.T) {
	raw := "/nonexistent/path/that/does/not/exist.go:1 pkg.Func"
	rf, err := Resolve(raw, 2)
	if err != nil {
		t.Fatalf("Resolve should not error on missing file: %v", err)
	}
	if !rf.External {
		t.Error("expected External=true for missing file")
	}
	if rf.Source != nil {
		t.Errorf("expected Source=nil for missing file, got %#v", rf.Source)
	}
}

func TestResolve_OutOfRangeLineMarkedExternal(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "short.go")
	if err := os.WriteFile(path, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw := path + ":99 pkg.Func"
	rf, err := Resolve(raw, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rf.External {
		t.Error("expected External=true when line is past EOF")
	}
}

func TestResolveMany_StopsOnParseError(t *testing.T) {
	raws := []string{
		"a.go:1 pkg.Func",
		"malformed",
		"b.go:2 pkg.Func",
	}
	out, err := ResolveMany(raws, 0)
	if err == nil {
		t.Fatal("expected error from second frame, got nil")
	}
	// We expect the first frame to have been processed before the error.
	if len(out) != 1 {
		t.Errorf("len(out) = %d, want 1 (only the first frame parsed)", len(out))
	}
}

func TestResolveMany_SkipsBlankFrames(t *testing.T) {
	raws := []string{
		"a.go:1 pkg.Func",
		"",
		"   ",
		"b.go:2 pkg.Func",
	}
	out, err := ResolveMany(raws, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("len(out) = %d, want 2 (blank entries skipped)", len(out))
	}
}

type ioErr string

func (e ioErr) Error() string { return string(e) }
