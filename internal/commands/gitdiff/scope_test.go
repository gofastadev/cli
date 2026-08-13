package gitdiff

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

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
