package stackresolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
