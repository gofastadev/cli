package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage for refactor.go's filesystem failure branches.
//
// These matter more than most error paths: the refactor MOVES a developer's
// source files, so a write or remove that fails silently loses code. Each
// branch must surface an error naming the path that failed.
//
// Errors are induced by path collision rather than permissions — a regular
// file where a directory must be (ENOTDIR), or a directory where a file must
// be (EISDIR). Both are deterministic and behave identically whether the suite
// runs as root or not, which chmod-based tricks do not.

// blockPath replaces the given path with a regular file, so any attempt to
// treat it as a directory fails with ENOTDIR.
func blockPath(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("blocker"), 0o644))
}

// occupyWithDir puts a directory where a file is expected, so os.WriteFile
// fails with EISDIR.
func occupyWithDir(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.MkdirAll(path, 0o755))
}

// --- migrateResource ---

func TestMigrateResource_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	// app/user must become a directory; a regular file there blocks it.
	blockPath(t, "app/user")

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir app/user")
}

func TestMigrateResource_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	// The first mapped file for `user` is app/user/dtos.go; a directory there
	// makes the write fail while the parent MkdirAll still succeeds.
	occupyWithDir(t, "app/user/dtos.go")

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/user/dtos.go")
}

// --- revertResource ---

func TestRevertResource_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()
	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	// Reversing needs app/dtos back; a regular file there blocks the mkdir.
	blockPath(t, "app/dtos")

	_, _, err = revertResource(r, fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir app/dtos")
}

func TestRevertResource_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()
	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	occupyWithDir(t, "app/dtos/user.dtos.go")

	_, _, err = revertResource(r, fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/dtos/user.dtos.go")
}

func TestRevertResource_MockWriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	mockPath := filepath.Join("testutil", "mocks", "user_service_mock.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mockPath), 0o755))
	require.NoError(t, os.WriteFile(mockPath, []byte("package mocks\n\nfunc Broken( {\n"), 0o644))

	_, _, err := revertResource(r, fixtureModulePath)
	require.Error(t, err, "an unparseable mock must abort rather than be written back mangled")
	assert.Contains(t, err.Error(), "featurize reverse")
}

// --- applySharedRelocations ---

func TestApplySharedRelocations_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	blockPath(t, "app/shared")

	_, err := applySharedRelocations()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir app/shared/dtos")
}

func TestApplySharedRelocations_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	occupyWithDir(t, "app/shared/dtos/aliases.go")

	_, err := applySharedRelocations()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/shared/dtos/aliases.go")
}

func TestApplySharedRelocationsReverse_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	_, err := applySharedRelocations()
	require.NoError(t, err)

	occupyWithDir(t, "app/dtos/aliases.go")

	_, err = applySharedRelocationsReverse()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/dtos/aliases.go")
}

// --- password generator ---

func TestRevertPasswordGenerator_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)
	_, err = relocatePasswordGenerator(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.NoError(t, err)

	occupyWithDir(t, "app/services/password_generator.go")

	_, err = revertPasswordGenerator()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password_generator.go")
}

func TestRevertPasswordGenerator_TransformFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	require.NoError(t, os.MkdirAll("app/user", 0o755))
	require.NoError(t, os.WriteFile("app/user/password_generator.go",
		[]byte("package user\n\nfunc Broken( {\n"), 0o644))

	_, err := revertPasswordGenerator()
	require.Error(t, err)
}

// --- cross-cutting patches ---

func TestApplyCrossCuttingPatches_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	occupyWithDir(t, "app/di/container.go")

	_, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/di/container.go")
}

func TestApplyCrossCuttingPatchesReverse_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	resources := []featurize.Resource{userResourceFixture()}
	_, err := applyCrossCuttingPatches(fixtureModulePath, resources)
	require.NoError(t, err)

	occupyWithDir(t, "app/di/container.go")

	_, err = applyCrossCuttingPatchesReverse(fixtureModulePath, resources)
	require.Error(t, err)
}

// --- config flip ---

func TestFlipLayoutInConfig_ReadFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	require.NoError(t, os.Remove("config.yaml"))

	require.Error(t, flipLayoutInConfig())
}

func TestFlipLayoutInConfigReverse_ReadFailureIsReported(t *testing.T) {
	inRenderedProject(t, "layered")
	require.NoError(t, os.Remove("config.yaml"))

	require.Error(t, flipLayoutInConfigReverse())
}
