package gitdiff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// failingCommand returns a *exec.Cmd whose Run() / Output() will fail
// because "false" exits with status 1 on every platform we support.
func failingCommand(_ context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.Command("false")
}

func TestChangedFiles_GitNotOnPath(t *testing.T) {
	saved := execLookPath
	execLookPath = func(_ string) (string, error) { return "", errors.New("no git") }
	t.Cleanup(func() { execLookPath = saved })

	_, err := ChangedFiles(context.Background(), "HEAD", Options{})
	require.Error(t, err)
}

func TestChangedFiles_DiffSinceRefFails(t *testing.T) {
	savedExec := execCommand
	savedLook := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/git", nil }
	// 1) is-inside-work-tree → "true"
	// 2) rev-parse --verify ref → succeeds
	// 3) diff --name-status ref...HEAD → fails
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{
		okCmd("true\n"),
		okCmd(""), // refResolves only needs no-error
		failCmd(),
	})
	t.Cleanup(func() { execCommand = savedExec; execLookPath = savedLook })

	_, err := ChangedFiles(context.Background(), "HEAD", Options{})
	require.Error(t, err)
}

func TestChangedFiles_StagedDiffFails(t *testing.T) {
	savedExec := execCommand
	savedLook := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/git", nil }
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{
		okCmd("true\n"), // is-inside
		failCmd(),       // git diff --cached fails (ref == "" so no ref-resolve call)
	})
	t.Cleanup(func() { execCommand = savedExec; execLookPath = savedLook })

	_, err := ChangedFiles(context.Background(), "", Options{})
	require.Error(t, err)
}

func TestChangedFiles_WorkingTreeDiffFails(t *testing.T) {
	savedExec := execCommand
	savedLook := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/git", nil }
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{
		okCmd("true\n"), // is-inside
		okCmd(""),       // staged
		failCmd(),       // unstaged fails
	})
	t.Cleanup(func() { execCommand = savedExec; execLookPath = savedLook })

	_, err := ChangedFiles(context.Background(), "", Options{})
	require.Error(t, err)
}

func TestChangedFiles_LsFilesFails(t *testing.T) {
	savedExec := execCommand
	savedLook := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/git", nil }
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{
		okCmd("true\n"), // is-inside
		okCmd(""),       // staged
		okCmd(""),       // unstaged
		failCmd(),       // ls-files fails
	})
	t.Cleanup(func() { execCommand = savedExec; execLookPath = savedLook })

	_, err := ChangedFiles(context.Background(), "", Options{})
	require.Error(t, err)
}

func TestRunGit_NoStderrSurfacesUnderlyingError(t *testing.T) {
	saved := execCommand
	// Command that exits non-zero AND writes nothing to stderr. `false`
	// fits the bill — on every platform it exits 1 with no output.
	execCommand = failingCommand
	t.Cleanup(func() { execCommand = saved })

	_, err := runGit(context.Background(), "doesnotmatter")
	require.Error(t, err)
	// The msg from `false` is empty, so we fall through to err.Error()
	// — non-empty by construction.
	require.NotEmpty(t, err.Error())
}

func TestFileSet_AddEmptyIsNoop(t *testing.T) {
	s := newFileSet()
	s.add("")
	require.Empty(t, s.sorted())
}

func TestAbsorbStatus_SkipsTooFewParts(t *testing.T) {
	s := newFileSet()
	// "M" with no tab → only 1 part; absorbStatus should skip.
	s.absorbStatus("M\n", false)
	require.Empty(t, s.sorted())
}

func TestAbsorbStatus_HandlesRenamesAndCopies(t *testing.T) {
	s := newFileSet()
	// R100  old.go  new.go  → take new.go
	// C75   src.go  copy.go → take copy.go
	// R100  shortrec      (only 2 parts) → ignored
	s.absorbStatus("R100\told.go\tnew.go\nC75\tsrc.go\tcopy.go\nR100\tshortrec\n", false)
	got := s.sorted()
	require.ElementsMatch(t, []string{"new.go", "copy.go"}, got)
}

// setupGitRepo creates a temp git repo with a known structure for testing.
// Returns the repo path so the test can chdir into it.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		// Don't taint the user's git config — set the test author here.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=tester",
			"GIT_AUTHOR_EMAIL=tester@example.com",
			"GIT_COMMITTER_NAME=tester",
			"GIT_COMMITTER_EMAIL=tester@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", args[0], args[1:], err, out)
		}
	}

	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("git", "init", "-q", "-b", "main")
	run("git", "config", "user.email", "tester@example.com")
	run("git", "config", "user.name", "tester")
	run("git", "config", "commit.gpgsign", "false")

	write("a.go", "package x\n")
	write("pkg/b.go", "package pkg\n")
	run("git", "add", ".")
	run("git", "commit", "-q", "-m", "initial")

	return dir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestChangedFiles_DetectsModifiedTrackedFile(t *testing.T) {
	dir := setupGitRepo(t)
	chdir(t, dir)

	// Modify an existing file but don't stage it.
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\nvar X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ChangedFiles(context.Background(), "HEAD", Options{})
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if !contains(got, "a.go") {
		t.Errorf("expected a.go in result, got %v", got)
	}
}

func TestChangedFiles_DetectsStagedFile(t *testing.T) {
	dir := setupGitRepo(t)
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\nvar X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", "a.go")

	got, err := ChangedFiles(context.Background(), "HEAD", Options{})
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if !contains(got, "a.go") {
		t.Errorf("expected staged a.go in result, got %v", got)
	}
}

func TestChangedFiles_DetectsUntrackedFile(t *testing.T) {
	dir := setupGitRepo(t)
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ChangedFiles(context.Background(), "HEAD", Options{})
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if !contains(got, "new.go") {
		t.Errorf("expected untracked new.go in result, got %v", got)
	}
}

func TestChangedFiles_SkipUntrackedHonored(t *testing.T) {
	dir := setupGitRepo(t)
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ChangedFiles(context.Background(), "HEAD", Options{SkipUntracked: true})
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if contains(got, "new.go") {
		t.Errorf("expected untracked new.go to be excluded, got %v", got)
	}
}

func TestChangedFiles_DeletedExcludedByDefault(t *testing.T) {
	dir := setupGitRepo(t)
	chdir(t, dir)

	if err := os.Remove(filepath.Join(dir, "a.go")); err != nil {
		t.Fatal(err)
	}

	got, err := ChangedFiles(context.Background(), "HEAD", Options{})
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if contains(got, "a.go") {
		t.Errorf("deleted file should be excluded by default, got %v", got)
	}
}

func TestChangedFiles_DeletedIncludedWithOption(t *testing.T) {
	dir := setupGitRepo(t)
	chdir(t, dir)

	if err := os.Remove(filepath.Join(dir, "a.go")); err != nil {
		t.Fatal(err)
	}

	got, err := ChangedFiles(context.Background(), "HEAD", Options{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if !contains(got, "a.go") {
		t.Errorf("deleted file should be included with IncludeDeleted, got %v", got)
	}
}

func TestChangedFiles_ErrorsOnUnknownRef(t *testing.T) {
	dir := setupGitRepo(t)
	chdir(t, dir)

	_, err := ChangedFiles(context.Background(), "does-not-exist", Options{})
	if err == nil {
		t.Fatal("expected error for unknown ref, got nil")
	}
}

func TestChangedFiles_ErrorsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	_, err := ChangedFiles(context.Background(), "HEAD", Options{})
	if err == nil {
		t.Fatal("expected error outside git repo, got nil")
	}
}

func TestFilterGoFiles(t *testing.T) {
	in := []string{"a.go", "b.txt", "pkg/c.go", "d.md"}
	got := FilterGoFiles(in)
	want := []string{"a.go", "pkg/c.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUniqueDirs(t *testing.T) {
	in := []string{"a.go", "pkg/b.go", "pkg/c.go", "other/d.go"}
	got := UniqueDirs(in)
	sort.Strings(got)
	want := []string{".", "other", "pkg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tester",
		"GIT_AUTHOR_EMAIL=tester@example.com",
		"GIT_COMMITTER_NAME=tester",
		"GIT_COMMITTER_EMAIL=tester@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", args, err, out)
	}
}
