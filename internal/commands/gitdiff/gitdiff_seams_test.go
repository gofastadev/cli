package gitdiff

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// failingCommand returns a *exec.Cmd whose Run() / Output() will fail
// because "false" exits with status 1 on every platform we support.
func failingCommand(_ context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.Command("false")
}

// stagedExecCommand returns a fake execCommand seam that hands out one
// fake command per call from a pre-baked queue. Lets a test choreograph
// successes followed by a single failure on the Nth call.
func stagedExecCommand(t *testing.T, plans []func() *exec.Cmd) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	var i int
	return func(_ context.Context, _ string, _ ...string) *exec.Cmd {
		if i >= len(plans) {
			t.Fatalf("execCommand called %d time(s); only %d staged", i+1, len(plans))
		}
		c := plans[i]()
		i++
		return c
	}
}

func okCmd(out string) func() *exec.Cmd { return func() *exec.Cmd { return exec.Command("printf", out) } }
func failCmd() func() *exec.Cmd        { return func() *exec.Cmd { return exec.Command("false") } }

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

// ─── scope.go ────────────────────────────────────────────────────────────

func TestPackagesForDirs_EmptyReturnsNil(t *testing.T) {
	got, err := PackagesForDirs(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestPackagesForDirs_HappyPath(t *testing.T) {
	saved := execCommand
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{
		okCmd("github.com/a/b\ngithub.com/c/d\n"),
	})
	t.Cleanup(func() { execCommand = saved })

	got, err := PackagesForDirs(context.Background(), []string{"a/b", "c/d"})
	require.NoError(t, err)
	require.Equal(t, []string{"github.com/a/b", "github.com/c/d"}, got)
}

func TestPackagesForDirs_GoListFails(t *testing.T) {
	saved := execCommand
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{failCmd()})
	t.Cleanup(func() { execCommand = saved })

	_, err := PackagesForDirs(context.Background(), []string{"a/b"})
	require.Error(t, err)
}

func TestReverseDeps_EmptyRootsReturnsNil(t *testing.T) {
	got, err := ReverseDeps(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestReverseDeps_HappyPath(t *testing.T) {
	saved := execCommand
	// Two packages: pkg/a imports root-pkg; pkg/b imports something else.
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{
		okCmd("pkg/a|root-pkg fmt strings \npkg/b|fmt strings \nbad-line-no-pipe\n"),
	})
	t.Cleanup(func() { execCommand = saved })

	got, err := ReverseDeps(context.Background(), []string{"root-pkg"})
	require.NoError(t, err)
	// root-pkg itself + pkg/a (which depends on it). pkg/b excluded.
	require.ElementsMatch(t, []string{"root-pkg", "pkg/a"}, got)
}

func TestReverseDeps_GoListFails(t *testing.T) {
	saved := execCommand
	execCommand = stagedExecCommand(t, []func() *exec.Cmd{failCmd()})
	t.Cleanup(func() { execCommand = saved })

	_, err := ReverseDeps(context.Background(), []string{"root-pkg"})
	require.Error(t, err)
}
