package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

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

// ----- helpers -----------------------------------------------------------

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
