package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage for the "removed the original but couldn't" branches.
//
// Every file move here is read → write → remove. If the remove fails after the
// write succeeded, the file now exists in BOTH layouts, which is a duplicate
// declaration and a broken build. The error has to surface rather than the
// move reporting success.
//
// Unlike the write failures, these cannot be induced by a path collision:
// os.Remove only fails on a path that was readable and writable a moment ago,
// which leaves the parent directory's write permission as the only lever. That
// means skipping under root, which ignores the mode bits entirely.

// lockDir removes write permission from a directory so entries inside it
// cannot be unlinked, then restores it for cleanup.
func lockDir(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can unlink from a write-protected directory")
	}
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, info.Mode().Perm()) })
}

func TestMigrateResource_RemoveFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	lockDir(t, "app/dtos") // holds the first mapped file, user.dtos.go

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing app/dtos/user.dtos.go")
}

func TestRevertResource_RemoveFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	r := userResourceFixture()
	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	lockDir(t, "app/user")

	_, _, err = revertResource(r, fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing app/user/")
}

func TestApplySharedRelocations_RemoveFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	lockDir(t, "app/dtos")

	_, err := applySharedRelocations()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing app/dtos/aliases.go")
}

func TestApplySharedRelocationsReverse_RemoveFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	_, err := applySharedRelocations()
	require.NoError(t, err)

	lockDir(t, "app/shared/dtos")

	_, err = applySharedRelocationsReverse()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing app/shared/dtos/aliases.go")
}

func TestRelocatePasswordGenerator_RemoveFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)

	lockDir(t, "app/services")

	_, err = relocatePasswordGenerator(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing app/services/password_generator.go")
}

func TestRevertPasswordGenerator_RemoveFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)
	_, err = relocatePasswordGenerator(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.NoError(t, err)

	lockDir(t, "app/user")

	_, err = revertPasswordGenerator()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing")
}

// TestMigrateResource_MockWriteFailureIsReported covers the mock write arm,
// which needs a readable-but-unwritable file for the same reason the
// cross-cutting write test does.
func TestMigrateResource_MockWriteFailureIsReported(t *testing.T) {
	requireNonRoot(t)
	inRenderedProject(t)

	mockPath := filepath.Join("testutil", "mocks", "user_service_mock.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mockPath), 0o755))
	require.NoError(t, os.WriteFile(mockPath, []byte(`package mocks

import svcInterfaces "`+fixtureModulePath+`/app/services/interfaces"

var _ svcInterfaces.UserServiceInterface = nil
`), 0o644))
	makeReadOnly(t, mockPath)

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), mockPath)
}

func TestRevertResource_MockWriteIsReportedWhenUnwritable(t *testing.T) {
	requireNonRoot(t)
	inRenderedProject(t)
	r := userResourceFixture()
	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	mockPath := filepath.Join("testutil", "mocks", "user_service_mock.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mockPath), 0o755))
	require.NoError(t, os.WriteFile(mockPath, []byte(`package mocks

import userpkg "`+fixtureModulePath+`/app/user"

var _ userpkg.UserServiceInterface = nil
`), 0o644))
	makeReadOnly(t, mockPath)

	_, _, err = revertResource(r, fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), mockPath)
}
