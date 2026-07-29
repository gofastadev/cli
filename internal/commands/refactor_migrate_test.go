package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage for refactor.go's file-moving half — the functions that physically
// restructure a project on disk. Each runs against a real rendered skeleton
// (see refactor_fixture_test.go), because the failures worth catching are the
// ones where an actual scaffold file has a shape the transform did not expect.

// --- migrateResource ---

// TestMigrateResource_MovesEveryMappedFile is the core of the forward
// refactor: every per-resource file listed in the mapping moves to its feature
// location and is deleted from the layered one. A file left behind in both
// places compiles as a duplicate declaration.
func TestMigrateResource_MovesEveryMappedFile(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	// Record which layered files the scaffold actually ships, so the
	// assertions below only cover files that were there to move.
	present := map[string]string{}
	for _, pair := range featurize.PerResourceMapping(r.Snake) {
		if fileExistsInFixture(t, pair.Layered) {
			present[pair.Layered] = pair.Feature
		}
	}
	require.NotEmpty(t, present, "the scaffold must ship layered user files to migrate")

	moved, patched, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)
	assert.Len(t, moved, len(present), "every present mapped file must be reported as moved")
	assert.NotNil(t, patched)

	for layered, feature := range present {
		assert.False(t, fileExistsInFixture(t, layered), "%s must be removed", layered)
		assert.True(t, fileExistsInFixture(t, feature), "%s must be created", feature)
	}
}

// TestMigrateResource_RewritesPackageDeclarations checks the moved files are
// transformed, not merely relocated: a file in app/user/ declaring `package
// services` would not compile.
func TestMigrateResource_RewritesPackageDeclarations(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	for _, path := range []string{
		"app/user/service.go",
		"app/user/repository.go",
		"app/user/controller.go",
	} {
		if !fileExistsInFixture(t, path) {
			continue
		}
		assert.Contains(t, readFixtureFile(t, path), "package user\n",
			"%s must declare the feature package", path)
	}
}

// TestMigrateResource_SkipsAbsentFilesSilently covers the read-error continue.
// The mapping lists files not every project has (a resource generated without
// tests, for instance); a missing one is not an error.
func TestMigrateResource_SkipsAbsentFilesSilently(t *testing.T) {
	inRenderedProject(t, "layered")
	r := featurize.Resource{Name: "Ghost", Snake: "ghost", Plural: "Ghosts"}

	moved, patched, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err, "a resource with no files on disk is a no-op, not a failure")
	assert.Empty(t, moved)
	assert.Empty(t, patched)
}

// TestMigrateResource_ReportsATransformFailure covers the error return: a
// layered file that will not parse must abort the migration rather than write
// a half-transformed tree.
func TestMigrateResource_ReportsATransformFailure(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	// Corrupt one mapped file so featurize cannot parse it.
	broken := "app/services/user.service.go"
	require.True(t, fileExistsInFixture(t, broken))
	require.NoError(t, os.WriteFile(broken, []byte("package services\n\nfunc Broken( {\n"), 0o644))

	_, _, err := migrateResource(r, fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "featurize")
}

// TestMigrateResource_TransformsMocks covers the mocks loop: a mock lives in
// testutil/mocks and stays there, but its qualifiers must follow the resource
// into the feature package.
func TestMigrateResource_TransformsMocks(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	mockPath := filepath.Join("testutil", "mocks", "user_service_mock.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mockPath), 0o755))
	require.NoError(t, os.WriteFile(mockPath, []byte(`package mocks

import (
	svcInterfaces "example.com/fixtureapp/app/services/interfaces"
)

type UserServiceMock struct{}

var _ svcInterfaces.UserServiceInterface = (*UserServiceMock)(nil)
`), 0o644))

	_, patched, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	assert.Contains(t, patched, mockPath)
	body := readFixtureFile(t, mockPath)
	assert.Contains(t, body, "userpkg.UserServiceInterface")
	assert.Contains(t, body, "package mocks", "the mock stays in package mocks")
}

// --- applySharedRelocations ---

// TestApplySharedRelocations_MovesAliasesFile covers the shared relocation:
// app/dtos/aliases.go becomes app/shared/dtos/aliases.go so the per-resource
// dtos files can move into their features without colliding.
func TestApplySharedRelocations_MovesAliasesFile(t *testing.T) {
	inRenderedProject(t, "layered")
	require.True(t, fileExistsInFixture(t, "app/dtos/aliases.go"),
		"the scaffold must ship the shared aliases file")

	before := readFixtureFile(t, "app/dtos/aliases.go")

	moved, err := applySharedRelocations()
	require.NoError(t, err)
	require.Len(t, moved, 1)

	assert.False(t, fileExistsInFixture(t, "app/dtos/aliases.go"))
	assert.True(t, fileExistsInFixture(t, "app/shared/dtos/aliases.go"))
	assert.Equal(t, before, readFixtureFile(t, "app/shared/dtos/aliases.go"),
		"a relocation moves bytes unchanged — the rewriting happens on the caller side")
}

func TestApplySharedRelocations_NoOpWhenAbsent(t *testing.T) {
	inRenderedProject(t, "layered")
	require.NoError(t, os.Remove("app/dtos/aliases.go"))

	moved, err := applySharedRelocations()
	require.NoError(t, err)
	assert.Empty(t, moved)
}

// TestApplySharedRelocationsReverse_MovesItBack pins the inverse.
func TestApplySharedRelocationsReverse_MovesItBack(t *testing.T) {
	inRenderedProject(t, "layered")

	_, err := applySharedRelocations()
	require.NoError(t, err)
	require.True(t, fileExistsInFixture(t, "app/shared/dtos/aliases.go"))

	moved, err := applySharedRelocationsReverse()
	require.NoError(t, err)
	require.Len(t, moved, 1)

	assert.True(t, fileExistsInFixture(t, "app/dtos/aliases.go"))
	assert.False(t, fileExistsInFixture(t, "app/shared/dtos/aliases.go"))
}

func TestApplySharedRelocationsReverse_NoOpWhenAbsent(t *testing.T) {
	inRenderedProject(t, "layered")

	moved, err := applySharedRelocationsReverse()
	require.NoError(t, err)
	assert.Empty(t, moved, "nothing to move back in a project that never went forward")
}

// --- password generator relocation ---

// TestRelocatePasswordGenerator_FollowsTheUserFeature covers the special case:
// the password generator is only consumed by the user feature, so it moves
// with it rather than staying in the shared services package.
func TestRelocatePasswordGenerator_FollowsTheUserFeature(t *testing.T) {
	inRenderedProject(t, "layered")
	const layered = "app/services/password_generator.go"
	require.True(t, fileExistsInFixture(t, layered))

	// Migrate first, matching runRefactorFeature's order. relocatePasswordGenerator
	// writes straight to app/user/ without creating it, so it relies on
	// migrateResource having made the directory — see the note on
	// TestRelocatePasswordGenerator_NeedsTheFeatureDirToExist.
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)

	moved, err := relocatePasswordGenerator(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.NoError(t, err)
	require.Len(t, moved, 1)

	assert.False(t, fileExistsInFixture(t, layered))
	assert.True(t, fileExistsInFixture(t, "app/user/password_generator.go"))
	assert.Contains(t, readFixtureFile(t, "app/user/password_generator.go"), "package user\n")
}

// TestRelocatePasswordGenerator_NoUserFeatureIsANoOp covers the guard: with no
// user resource there is nothing for the generator to follow.
func TestRelocatePasswordGenerator_NoUserFeatureIsANoOp(t *testing.T) {
	inRenderedProject(t, "layered")

	moved, err := relocatePasswordGenerator(fixtureModulePath,
		[]featurize.Resource{{Name: "Order", Snake: "order", Plural: "Orders"}})
	require.NoError(t, err)
	assert.Empty(t, moved)
	assert.True(t, fileExistsInFixture(t, "app/services/password_generator.go"),
		"the generator must stay put when no user feature exists")
}

// TestRevertPasswordGenerator_MovesItBack pins the inverse.
func TestRevertPasswordGenerator_MovesItBack(t *testing.T) {
	inRenderedProject(t, "layered")

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)
	_, err = relocatePasswordGenerator(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.NoError(t, err)

	moved, err := revertPasswordGenerator()
	require.NoError(t, err)
	require.Len(t, moved, 1)

	assert.True(t, fileExistsInFixture(t, "app/services/password_generator.go"))
	assert.False(t, fileExistsInFixture(t, "app/user/password_generator.go"))
	assert.Contains(t, readFixtureFile(t, "app/services/password_generator.go"), "package services\n")
}

func TestRevertPasswordGenerator_NoOpWhenAbsent(t *testing.T) {
	inRenderedProject(t, "layered")

	moved, err := revertPasswordGenerator()
	require.NoError(t, err)
	assert.Empty(t, moved)
}

// --- cross-cutting patches ---

// TestApplyCrossCuttingPatches_RewritesTheSharedFiles covers the container /
// wire / index-routes / core-providers rewrite. These four files reference
// every resource, so they are patched rather than moved.
func TestApplyCrossCuttingPatches_RewritesTheSharedFiles(t *testing.T) {
	inRenderedProject(t, "layered")

	patched, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.NoError(t, err)
	require.NotEmpty(t, patched)

	assert.Contains(t, patched, "app/di/container.go")
	container := readFixtureFile(t, "app/di/container.go")
	assert.Contains(t, container, "userpkg", "the container must reference the feature package")
}

// TestApplyCrossCuttingPatches_ReportsATransformFailure covers the error
// return when one of the shared files will not parse.
func TestApplyCrossCuttingPatches_ReportsATransformFailure(t *testing.T) {
	inRenderedProject(t, "layered")
	require.NoError(t, os.WriteFile("app/di/container.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	_, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transform")
}

// TestApplyCrossCuttingPatchesReverse_RewritesTheSharedFiles pins the inverse.
func TestApplyCrossCuttingPatchesReverse_RewritesTheSharedFiles(t *testing.T) {
	inRenderedProject(t, "layered")
	resources := []featurize.Resource{userResourceFixture()}

	_, err := applyCrossCuttingPatches(fixtureModulePath, resources)
	require.NoError(t, err)

	patched, err := applyCrossCuttingPatchesReverse(fixtureModulePath, resources)
	require.NoError(t, err)
	require.NotEmpty(t, patched)

	container := readFixtureFile(t, "app/di/container.go")
	assert.NotContains(t, container, "userpkg",
		"back in layered layout the feature alias must be gone")
}

func TestApplyCrossCuttingPatchesReverse_ReportsATransformFailure(t *testing.T) {
	inRenderedProject(t, "layered")
	require.NoError(t, os.WriteFile("app/di/container.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	_, err := applyCrossCuttingPatchesReverse(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.Error(t, err)
}

// --- directory pruning ---

// TestPruneEmptyLayeredDirs_RemovesOnlyEmptyDirectories covers the forward
// prune. A layered directory that still holds a file belongs to code the
// refactor did not touch and must survive.
func TestPruneEmptyLayeredDirs_RemovesOnlyEmptyDirectories(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	// Leave a stray file behind in one of the directories being pruned.
	require.NoError(t, os.MkdirAll("app/repositories", 0o755))
	require.NoError(t, os.WriteFile("app/repositories/keepme.go", []byte("package repositories\n"), 0o644))

	pruneEmptyLayeredDirs()

	assert.True(t, fileExistsInFixture(t, "app/repositories/keepme.go"),
		"a directory with remaining files must not be pruned")
}

// TestPruneEmptyFeatureDirs_RemovesOnlyEmptyDirectories covers the reverse
// prune.
func TestPruneEmptyFeatureDirs_RemovesOnlyEmptyDirectories(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)
	_, _, err = revertResource(r, fixtureModulePath)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll("app/user", 0o755))
	require.NoError(t, os.WriteFile("app/user/handwritten.go", []byte("package user\n"), 0o644))

	pruneEmptyFeatureDirs([]featurize.Resource{r})

	assert.True(t, fileExistsInFixture(t, "app/user/handwritten.go"),
		"a feature directory the developer still uses must survive the prune")
}

// --- config layout flag ---

// TestFlipLayoutInConfig_WritesTheFeatureValue covers the config rewrite.
// layout.Detect() treats config.yaml as authoritative, so a refactor that
// moved the files but left this key stale would make every later generator
// write to the wrong place.
func TestFlipLayoutInConfig_WritesTheFeatureValue(t *testing.T) {
	inRenderedProject(t, "layered")

	require.NoError(t, flipLayoutInConfig())

	cfg := readFixtureFile(t, "config.yaml")
	assert.Contains(t, cfg, "layout: feature")
	assert.NotContains(t, cfg, "layout: layered")
}

// TestFlipLayoutInConfigReverse_WritesTheLayeredValue pins the inverse.
func TestFlipLayoutInConfigReverse_WritesTheLayeredValue(t *testing.T) {
	inRenderedProject(t, "layered")

	require.NoError(t, flipLayoutInConfig())
	require.NoError(t, flipLayoutInConfigReverse())

	cfg := readFixtureFile(t, "config.yaml")
	assert.Contains(t, cfg, "layout: layered")
	assert.NotContains(t, cfg, "layout: feature")
}

// --- round trip ---

// TestRefactor_ForwardThenBackRestoresEveryPath is the property that matters
// most for a refactor a developer might run and then undo: after a full
// forward-and-back cycle every file is at the path it started from.
func TestRefactor_ForwardThenBackRestoresEveryPath(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	before := map[string]bool{}
	for _, pair := range featurize.PerResourceMapping(r.Snake) {
		if fileExistsInFixture(t, pair.Layered) {
			before[pair.Layered] = true
		}
	}
	require.NotEmpty(t, before)

	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)
	_, _, err = revertResource(r, fixtureModulePath)
	require.NoError(t, err)

	for path := range before {
		assert.True(t, fileExistsInFixture(t, path), "%s must be restored", path)
		body := readFixtureFile(t, path)
		assert.False(t, strings.HasPrefix(body, "package user\n"),
			"%s must not still declare the feature package", path)
	}
}

// TestRevertResource_SkipsAbsentFilesSilently covers the read-error continue on
// the reverse side.
func TestRevertResource_SkipsAbsentFilesSilently(t *testing.T) {
	inRenderedProject(t, "layered")

	moved, patched, err := revertResource(
		featurize.Resource{Name: "Ghost", Snake: "ghost", Plural: "Ghosts"}, fixtureModulePath)
	require.NoError(t, err)
	assert.Empty(t, moved)
	assert.Empty(t, patched)
}

// TestRevertResource_ReportsATransformFailure covers its error return.
func TestRevertResource_ReportsATransformFailure(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile("app/user/service.go",
		[]byte("package user\n\nfunc Broken( {\n"), 0o644))

	_, _, err = revertResource(r, fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "featurize reverse")
}

// TestRevertResource_TransformsMocks covers the reverse mocks loop.
func TestRevertResource_TransformsMocks(t *testing.T) {
	inRenderedProject(t, "layered")
	r := userResourceFixture()

	mockPath := filepath.Join("testutil", "mocks", "user_repository_mock.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mockPath), 0o755))
	require.NoError(t, os.WriteFile(mockPath, []byte(`package mocks

import (
	userpkg "example.com/fixtureapp/app/user"
)

type UserRepositoryMock struct{}

var _ userpkg.UserRepositoryInterface = (*UserRepositoryMock)(nil)
`), 0o644))

	_, patched, err := revertResource(r, fixtureModulePath)
	require.NoError(t, err)

	assert.Contains(t, patched, mockPath)
	assert.Contains(t, readFixtureFile(t, mockPath), "repoInterfaces.UserRepositoryInterface")
}

// TestRelocatePasswordGenerator_NeedsTheFeatureDirToExist documents an
// ordering dependency rather than asserting it is desirable.
//
// Unlike migrateResource, which does an os.MkdirAll before every write,
// relocatePasswordGenerator writes straight to app/user/. It works only
// because runRefactorFeature calls migrateResource first. Called on its own —
// which nothing does today — it fails. Pinned so a future caller reordering
// these steps gets a test failure instead of a confusing runtime error.
func TestRelocatePasswordGenerator_NeedsTheFeatureDirToExist(t *testing.T) {
	inRenderedProject(t, "layered")

	_, err := relocatePasswordGenerator(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err, "without migrateResource having created app/user/, the write fails")
	assert.Contains(t, err.Error(), "writing app/user/password_generator.go")
}
