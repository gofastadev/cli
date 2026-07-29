package commands

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// layeredRefactorFixture chdir's into a fresh temp dir laid out as a
// minimal layered gofasta project: go.mod, config.yaml (project.layout:
// layered), a User model + service, and the standalone
// password_generator.go that the refactor relocates into the user
// feature. Nothing here has to compile — the refactor status/dry-run
// paths under test only Stat files and read config/go.mod.
func layeredRefactorFixture(t *testing.T) {
	t.Helper()
	chdirTemp(t)
	writeRefactorFile(t, "go.mod", "module example.com/proj\n\ngo 1.25\n")
	writeRefactorFile(t, "config.yaml", "project:\n  layout: layered\n\nserver:\n  port: \"8080\"\n")
	writeRefactorFile(t, "app/models/user.model.go", "package models\n\ntype User struct{}\n")
	writeRefactorFile(t, "app/services/user.service.go", "package services\n")
	writeRefactorFile(t, "app/services/password_generator.go", "package services\n")
}

// featureRefactorFixture chdir's into a fresh temp dir laid out as a
// minimal feature-package project: config.layout: feature plus an
// app/user/ feature dir with a service.go so discoverFeatureResources
// picks it up.
func featureRefactorFixture(t *testing.T) {
	t.Helper()
	chdirTemp(t)
	writeRefactorFile(t, "go.mod", "module example.com/proj\n\ngo 1.25\n")
	writeRefactorFile(t, "config.yaml", "project:\n  layout: feature\n\nserver:\n  port: \"8080\"\n")
	writeRefactorFile(t, "app/user/service.go", "package user\n")
	writeRefactorFile(t, "app/user/password_generator.go", "package user\n")
}

// snapshotTree returns a map of every regular file (relative to cwd,
// excluding .git) to its contents. Used to assert a dry-run leaves the
// working tree untouched.
func snapshotTree(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[path] = string(data)
		return nil
	})
	require.NoError(t, err)
	return out
}

// gitInitOrSkip initializes a git repo in cwd so requireCleanGitTree has
// something to inspect. Skips the test when git is not installed — the
// guard is environment-gated, not a logic branch we can stub (refactor.go
// shells out to git directly).
func gitInitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed; requireCleanGitTree guard needs a real repo")
	}
	require.NoError(t, exec.Command("git", "init").Run())
}

// setRefactorFlags sets the three shared flags on a refactor cobra
// command and restores them afterwards, so calling runRefactorFeature /
// runRefactorLayered directly exercises the real flag plumbing.
func setRefactorFlags(t *testing.T, dryRun, force bool) {
	t.Helper()
	b := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	for _, cmd := range []*struct {
		name  string
		value bool
	}{{"all", true}, {"dry-run", dryRun}, {"force", force}} {
		require.NoError(t, refactorFeatureCmd.Flags().Set(cmd.name, b(cmd.value)))
		require.NoError(t, refactorLayeredCmd.Flags().Set(cmd.name, b(cmd.value)))
	}
	t.Cleanup(func() {
		for _, n := range []string{"all", "dry-run", "force"} {
			_ = refactorFeatureCmd.Flags().Set(n, "false")
			_ = refactorLayeredCmd.Flags().Set(n, "false")
		}
	})
}

func TestRunRefactorStatus_Layered(t *testing.T) {
	layeredRefactorFixture(t)
	withJSONMode(t)
	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})
	var res refactorStatusResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.True(t, res.Success)
	assert.Equal(t, "layered", res.Layout)
	assert.Equal(t, "config.yaml", res.LayoutSource)
	assert.Equal(t, "feature", res.MigrationTarget)
	assert.Contains(t, res.Resources, "User")
	assert.Equal(t, "gofasta refactor feature --all", res.MigrationCmd)
}

func TestRunRefactorStatus_Feature(t *testing.T) {
	featureRefactorFixture(t)
	withJSONMode(t)
	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})
	var res refactorStatusResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.True(t, res.Success)
	assert.Equal(t, "feature", res.Layout)
	assert.Equal(t, "layered", res.MigrationTarget)
	assert.Contains(t, res.Resources, "User")
	assert.Equal(t, "gofasta refactor layered --all", res.MigrationCmd)
}

func TestRunRefactorStatus_NotAProject(t *testing.T) {
	chdirTemp(t) // empty dir: no config.yaml, no app/models
	withJSONMode(t)
	out := captureStdout(t, func() {
		require.Error(t, runRefactorStatus(refactorStatusCmd, nil))
	})
	var res refactorStatusResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.False(t, res.Success)
	assert.Equal(t, "unknown", res.Layout)
}

func TestRunRefactorFeature_DryRunPlansMovesWithoutWriting(t *testing.T) {
	layeredRefactorFixture(t)
	setRefactorFlags(t, true /*dry-run*/, false /*force*/)

	before := snapshotTree(t)
	out := captureStdout(t, func() {
		require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
	})
	after := snapshotTree(t)

	// The plan lists the per-resource service move and the standalone
	// password_generator relocation.
	assert.Contains(t, out, "app/services/user.service.go")
	assert.Contains(t, out, "app/services/password_generator.go → app/user/password_generator.go")

	// A dry-run must not touch the working tree.
	assert.Equal(t, before, after, "dry-run modified the working tree")
}

func TestRunRefactorFeature_DirtyTreeGuardBlocks(t *testing.T) {
	layeredRefactorFixture(t)
	gitInitOrSkip(t) // untracked fixture files => dirty tree
	setRefactorFlags(t, false /*dry-run*/, false /*force*/)

	err := runRefactorFeature(refactorFeatureCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted changes")
}

func TestRunRefactorFeature_ForceBypassesDirtyGuard(t *testing.T) {
	layeredRefactorFixture(t)
	gitInitOrSkip(t) // dirty tree, but --force skips the guard
	// --force + --dry-run: guard is bypassed, and dry-run returns before
	// any writes, so the run succeeds and leaves the tree untouched.
	setRefactorFlags(t, true /*dry-run*/, true /*force*/)

	before := snapshotTree(t)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
	after := snapshotTree(t)
	assert.Equal(t, before, after, "force+dry-run modified the working tree")
}

func TestRunRefactorLayered_RejectsLayeredProject(t *testing.T) {
	layeredRefactorFixture(t) // config.layout: layered, so "nothing to unwind"
	setRefactorFlags(t, false, false)

	err := runRefactorLayered(refactorLayeredCmd, nil)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "not in feature-package layout")
}

func TestToPascalCaseSimple_SplitsUnderscoreAndDash(t *testing.T) {
	// Mirrors internal/generate/stringutil.go's toPascalCase so
	// `gofasta refactor` and `gofasta g scaffold` agree on names.
	assert.Equal(t, "OrderItem", toPascalCaseSimple("order_item"))
	assert.Equal(t, "OrderItem", toPascalCaseSimple("order-item"))
	assert.Equal(t, "ApiKeyToken", toPascalCaseSimple("api-key_token"))
}

// TestMigrateResource_MovesEveryMappedFile is the core of the forward
// refactor: every per-resource file listed in the mapping moves to its feature
// location and is deleted from the layered one. A file left behind in both
// places compiles as a duplicate declaration.
func TestMigrateResource_MovesEveryMappedFile(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)
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
	inRenderedProject(t)
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
	inRenderedProject(t)
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
	inRenderedProject(t)
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

// TestApplySharedRelocations_MovesAliasesFile covers the shared relocation:
// app/dtos/aliases.go becomes app/shared/dtos/aliases.go so the per-resource
// dtos files can move into their features without colliding.
func TestApplySharedRelocations_MovesAliasesFile(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)
	require.NoError(t, os.Remove("app/dtos/aliases.go"))

	moved, err := applySharedRelocations()
	require.NoError(t, err)
	assert.Empty(t, moved)
}

// TestApplySharedRelocationsReverse_MovesItBack pins the inverse.
func TestApplySharedRelocationsReverse_MovesItBack(t *testing.T) {
	inRenderedProject(t)

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
	inRenderedProject(t)

	moved, err := applySharedRelocationsReverse()
	require.NoError(t, err)
	assert.Empty(t, moved, "nothing to move back in a project that never went forward")
}

// TestRelocatePasswordGenerator_FollowsTheUserFeature covers the special case:
// the password generator is only consumed by the user feature, so it moves
// with it rather than staying in the shared services package.
func TestRelocatePasswordGenerator_FollowsTheUserFeature(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)

	moved, err := relocatePasswordGenerator(fixtureModulePath,
		[]featurize.Resource{{Name: "Order", Snake: "order", Plural: "Orders"}})
	require.NoError(t, err)
	assert.Empty(t, moved)
	assert.True(t, fileExistsInFixture(t, "app/services/password_generator.go"),
		"the generator must stay put when no user feature exists")
}

// TestRevertPasswordGenerator_MovesItBack pins the inverse.
func TestRevertPasswordGenerator_MovesItBack(t *testing.T) {
	inRenderedProject(t)

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
	inRenderedProject(t)

	moved, err := revertPasswordGenerator()
	require.NoError(t, err)
	assert.Empty(t, moved)
}

// TestApplyCrossCuttingPatches_RewritesTheSharedFiles covers the container /
// wire / index-routes / core-providers rewrite. These four files reference
// every resource, so they are patched rather than moved.
func TestApplyCrossCuttingPatches_RewritesTheSharedFiles(t *testing.T) {
	inRenderedProject(t)

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
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("app/di/container.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	_, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transform")
}

// TestApplyCrossCuttingPatchesReverse_RewritesTheSharedFiles pins the inverse.
func TestApplyCrossCuttingPatchesReverse_RewritesTheSharedFiles(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("app/di/container.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	_, err := applyCrossCuttingPatchesReverse(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.Error(t, err)
}

// TestPruneEmptyLayeredDirs_RemovesOnlyEmptyDirectories covers the forward
// prune. A layered directory that still holds a file belongs to code the
// refactor did not touch and must survive.
func TestPruneEmptyLayeredDirs_RemovesOnlyEmptyDirectories(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)
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

// TestFlipLayoutInConfig_WritesTheFeatureValue covers the config rewrite.
// layout.Detect() treats config.yaml as authoritative, so a refactor that
// moved the files but left this key stale would make every later generator
// write to the wrong place.
func TestFlipLayoutInConfig_WritesTheFeatureValue(t *testing.T) {
	inRenderedProject(t)

	require.NoError(t, flipLayoutInConfig())

	cfg := readFixtureFile(t, "config.yaml")
	assert.Contains(t, cfg, "layout: feature")
	assert.NotContains(t, cfg, "layout: layered")
}

// TestFlipLayoutInConfigReverse_WritesTheLayeredValue pins the inverse.
func TestFlipLayoutInConfigReverse_WritesTheLayeredValue(t *testing.T) {
	inRenderedProject(t)

	require.NoError(t, flipLayoutInConfig())
	require.NoError(t, flipLayoutInConfigReverse())

	cfg := readFixtureFile(t, "config.yaml")
	assert.Contains(t, cfg, "layout: layered")
	assert.NotContains(t, cfg, "layout: feature")
}

// TestRefactor_ForwardThenBackRestoresEveryPath is the property that matters
// most for a refactor a developer might run and then undo: after a full
// forward-and-back cycle every file is at the path it started from.
func TestRefactor_ForwardThenBackRestoresEveryPath(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)

	moved, patched, err := revertResource(
		featurize.Resource{Name: "Ghost", Snake: "ghost", Plural: "Ghosts"}, fixtureModulePath)
	require.NoError(t, err)
	assert.Empty(t, moved)
	assert.Empty(t, patched)
}

// TestRevertResource_ReportsATransformFailure covers its error return.
func TestRevertResource_ReportsATransformFailure(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)
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
	inRenderedProject(t)

	_, err := relocatePasswordGenerator(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err, "without migrateResource having created app/user/, the write fails")
	assert.Contains(t, err.Error(), "writing app/user/password_generator.go")
}

func TestToSnakeCaseSimple(t *testing.T) {
	cases := map[string]string{
		"User":          "user",
		"PurchaseOrder": "purchase_order",
		"HTTPServer":    "h_t_t_p_server",
		"already_snake": "already_snake",
		"A":             "a",
		"":              "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, toSnakeCaseSimple(in))
		})
	}
}

// TestPluralizeSimple covers each arm of the pluralization switch. The plural
// lands in generated type names (ListUsersFilter), so a wrong form produces
// code that does not match what the generators emit.
func TestPluralizeSimple(t *testing.T) {
	cases := map[string]string{
		"User":     "Users",
		"Category": "Categories", // consonant + y
		"Day":      "Days",       // vowel + y — not "Daies"
		"Address":  "Addresses",  // s
		"Box":      "Boxes",      // x
		"Buzz":     "Buzzes",     // z
		"Batch":    "Batches",    // ch
		"Dish":     "Dishes",     // sh
		"Y":        "Ys",         // too short for the y rule
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, pluralizeSimple(in))
		})
	}
}

func TestIsVowel(t *testing.T) {
	for _, r := range []rune{'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U'} {
		assert.True(t, isVowel(r), "%c must be a vowel", r)
	}
	for _, r := range []rune{'b', 'y', 'Z', '1', '_'} {
		assert.False(t, isVowel(r), "%c must not be a vowel", r)
	}
}

func TestReadModulePath(t *testing.T) {
	inRenderedProject(t)
	got, err := readModulePath()
	require.NoError(t, err)
	assert.Equal(t, fixtureModulePath, got)
}

func TestReadModulePath_NoGoMod(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove("go.mod"))

	_, err := readModulePath()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go.mod")
}

func TestReadModulePath_NoModuleDirective(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("go.mod", []byte("go 1.25.0\n"), 0o644))

	_, err := readModulePath()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module directive")
}

// TestRunGoCommand covers the thin exec wrapper in both directions. `go env`
// is used because it is fast, side-effect free, and always available.
func TestRunGoCommand(t *testing.T) {
	assert.NoError(t, runGoCommand("env", "GOPATH"))
	assert.Error(t, runGoCommand("definitely-not-a-go-subcommand"),
		"a failing go invocation must surface as an error")
}

func TestResolveRefactorResources_NamedResource(t *testing.T) {
	inRenderedProject(t)

	got, err := resolveRefactorResources([]string{"User"}, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, featurize.Resource{Name: "User", Snake: "user", Plural: "Users"}, got[0])
}

// TestResolveRefactorResources_UnknownResource covers the existence check: the
// refactor must refuse a name with no model file rather than silently doing
// nothing and reporting success.
func TestResolveRefactorResources_UnknownResource(t *testing.T) {
	inRenderedProject(t)

	_, err := resolveRefactorResources([]string{"Ghost"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/models/ghost.model.go")
}

func TestResolveRefactorResources_NoArgsWithoutAll(t *testing.T) {
	inRenderedProject(t)

	_, err := resolveRefactorResources(nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all")
}

func TestResolveRefactorResources_AllDiscoversFromModels(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("app/models/purchase_order.model.go",
		[]byte("package models\n\ntype PurchaseOrder struct{}\n"), 0o644))

	got, err := resolveRefactorResources(nil, true)
	require.NoError(t, err)

	names := map[string]featurize.Resource{}
	for _, r := range got {
		names[r.Name] = r
	}
	require.Contains(t, names, "User")
	require.Contains(t, names, "PurchaseOrder")
	assert.Equal(t, "purchase_order", names["PurchaseOrder"].Snake)
	assert.Equal(t, "PurchaseOrders", names["PurchaseOrder"].Plural)
}

// TestDiscoverResourcesFromModels_IgnoresNonModelEntries covers the filter:
// only <snake>.model.go files name a resource.
func TestDiscoverResourcesFromModels_IgnoresNonModelEntries(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("app/models/helpers.go", []byte("package models\n"), 0o644))
	require.NoError(t, os.MkdirAll("app/models/subdir", 0o755))

	got, err := discoverResourcesFromModels()
	require.NoError(t, err)
	for _, r := range got {
		assert.NotEqual(t, "helpers", r.Snake)
		assert.NotEqual(t, "subdir", r.Snake)
	}
}

func TestDiscoverResourcesFromModels_EmptyDir(t *testing.T) {
	inRenderedProject(t)
	entries, err := os.ReadDir("app/models")
	require.NoError(t, err)
	for _, e := range entries {
		require.NoError(t, os.RemoveAll(filepath.Join("app/models", e.Name())))
	}

	_, err = discoverResourcesFromModels()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no layered resources")
}

func TestDiscoverResourcesFromModels_MissingDir(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.RemoveAll("app/models"))

	_, err := discoverResourcesFromModels()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/models")
}

func TestResolveLayeredRevertResources_NamedFeature(t *testing.T) {
	inRenderedProject(t)
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)

	got, err := resolveLayeredRevertResources([]string{"User"}, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "user", got[0].Snake)
}

func TestResolveLayeredRevertResources_UnknownFeature(t *testing.T) {
	inRenderedProject(t)

	_, err := resolveLayeredRevertResources([]string{"Ghost"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/ghost/")
}

func TestResolveLayeredRevertResources_NoArgsWithoutAll(t *testing.T) {
	inRenderedProject(t)

	_, err := resolveLayeredRevertResources(nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all")
}

func TestResolveLayeredRevertResources_AllDiscoversFeatures(t *testing.T) {
	inRenderedProject(t)
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)

	got, err := resolveLayeredRevertResources(nil, true)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.Equal(t, "user", got[0].Snake)
}

// TestDiscoverFeatureResources_SkipsSharedDirs covers the exclusion list and
// the service.go heuristic: a directory under app/ is only a feature if it
// holds a service.go and is not a known shared concern.
func TestDiscoverFeatureResources_SkipsSharedDirs(t *testing.T) {
	inRenderedProject(t)
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)

	// A shared dir that would otherwise look like a feature.
	require.NoError(t, os.MkdirAll("app/shared", 0o755))
	require.NoError(t, os.WriteFile("app/shared/service.go", []byte("package shared\n"), 0o644))
	// A plain directory with no service.go.
	require.NoError(t, os.MkdirAll("app/notes", 0o755))
	require.NoError(t, os.WriteFile("app/notes/notes.go", []byte("package notes\n"), 0o644))
	// A stray file directly under app/.
	require.NoError(t, os.WriteFile("app/README.md", []byte("notes\n"), 0o644))

	got, err := discoverFeatureResources()
	require.NoError(t, err)

	for _, r := range got {
		assert.NotEqual(t, "shared", r.Snake, "shared concerns are not features")
		assert.NotEqual(t, "notes", r.Snake, "a dir without service.go is not a feature")
	}
}

func TestDiscoverFeatureResources_NoFeatures(t *testing.T) {
	inRenderedProject(t)

	_, err := discoverFeatureResources()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no feature directories")
}

func TestDiscoverFeatureResources_MissingAppDir(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.RemoveAll("app"))

	_, err := discoverFeatureResources()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/")
}

func TestRequireLayeredProject(t *testing.T) {
	inRenderedProject(t)
	assert.NoError(t, requireLayeredProject(), "a freshly scaffolded layered project is eligible")

	require.NoError(t, flipLayoutInConfig())
	err := requireLayeredProject()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already feature-package")
}

func TestRequireLayeredProject_NoModelsDir(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.RemoveAll("app/models"))

	err := requireLayeredProject()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/models/")
}

func TestRequireFeatureProject(t *testing.T) {
	inRenderedProject(t)

	err := requireFeatureProject()
	require.Error(t, err, "a layered project has nothing to unwind")
	assert.Contains(t, err.Error(), "not in feature-package layout")

	require.NoError(t, flipLayoutInConfig())
	assert.NoError(t, requireFeatureProject())
}

func TestRefactorResolvesUser(t *testing.T) {
	assert.True(t, refactorResolvesUser([]featurize.Resource{
		{Snake: "order"}, {Snake: "user"},
	}))
	assert.False(t, refactorResolvesUser([]featurize.Resource{{Snake: "order"}}))
	assert.False(t, refactorResolvesUser(nil))
}

// TestRunRefactorFeature_MigratesTheWholeProject is the forward orchestrator's
// happy path: after it returns, the project is in feature shape on disk and
// config.yaml says so.
func TestRunRefactorFeature_MigratesTheWholeProject(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false /*dry-run*/, false /*all*/)
	calls := stubGoCommands(t, nil)

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	// Files moved into the feature package.
	assert.True(t, fileExistsInFixture(t, "app/user/service.go"))
	assert.False(t, fileExistsInFixture(t, "app/services/user.service.go"))

	// Shared relocation and the password generator followed.
	assert.True(t, fileExistsInFixture(t, "app/shared/dtos/aliases.go"))
	assert.True(t, fileExistsInFixture(t, "app/user/password_generator.go"))

	// Cross-cutting files patched.
	assert.Contains(t, readFixtureFile(t, "app/di/container.go"), "userpkg")

	// Config flipped — layout.Detect treats this as authoritative.
	assert.Contains(t, readFixtureFile(t, "config.yaml"), "layout: feature")

	// Wire is regenerated BEFORE the build, because the stale wire_gen.go
	// still references the layered import paths.
	require.Len(t, *calls, 2)
	assert.Equal(t, []string{"tool", "wire", "./app/di/"}, (*calls)[0])
	assert.Equal(t, []string{"build", "./..."}, (*calls)[1])
}

// TestRunRefactorFeature_AllMigratesEveryResource covers the --all path
// through the orchestrator.
func TestRunRefactorFeature_AllMigratesEveryResource(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, true)
	stubGoCommands(t, nil)

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
	assert.True(t, fileExistsInFixture(t, "app/user/service.go"))
}

// TestRunRefactorFeature_WireFailureAborts covers the abort path. The message
// has to point at recovery, because the tree is already half-rewritten.
func TestRunRefactorFeature_WireFailureAborts(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, errors.New("wire blew up"))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wire generation failed")
	assert.Contains(t, err.Error(), "git restore",
		"an aborted refactor must tell the developer how to get back")
}

// TestRunRefactorFeature_BuildFailureAborts covers the second abort: wire
// succeeds, the build does not.
func TestRunRefactorFeature_BuildFailureAborts(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)

	orig := runGoCommandFn
	t.Cleanup(func() { runGoCommandFn = orig })
	runGoCommandFn = func(args ...string) error {
		if len(args) > 0 && args[0] == "build" {
			return errors.New("build blew up")
		}
		return nil
	}

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go build failed after migration")
}

// TestRunRefactorFeature_RemovesStaleWireGen covers the deliberate delete: the
// generated wire_gen.go still imports layered packages, and wire needs every
// file in the package to compile before it will regenerate.
func TestRunRefactorFeature_RemovesStaleWireGen(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)

	require.NoError(t, os.MkdirAll("app/di", 0o755))
	require.NoError(t, os.WriteFile("app/di/wire_gen.go",
		[]byte("package di\n\n// references layered packages\n"), 0o644))

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))
	assert.False(t, fileExistsInFixture(t, "app/di/wire_gen.go"),
		"the stale generated file must be removed before wire runs")
}

// TestRunRefactorFeature_UnknownResourceFails covers the resolution error
// surfacing through the orchestrator.
func TestRunRefactorFeature_UnknownResourceFails(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)

	err := runRefactorFeature(refactorFeatureCmd, []string{"Ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost.model.go")
}

// TestRunRefactorLayered_UnwindsTheWholeProject is the reverse orchestrator's
// happy path, run on a project that was migrated forward first.
func TestRunRefactorLayered_UnwindsTheWholeProject(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	calls := stubGoCommands(t, nil)

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))
	require.NoError(t, runRefactorLayered(refactorLayeredCmd, []string{"User"}))

	// Files are back in their layered homes.
	assert.True(t, fileExistsInFixture(t, "app/services/user.service.go"))
	assert.False(t, fileExistsInFixture(t, "app/user/service.go"))

	// Shared relocation and password generator reversed.
	assert.True(t, fileExistsInFixture(t, "app/dtos/aliases.go"))
	assert.True(t, fileExistsInFixture(t, "app/services/password_generator.go"))

	// Config flipped back.
	assert.Contains(t, readFixtureFile(t, "config.yaml"), "layout: layered")

	// Two go invocations per direction.
	assert.Len(t, *calls, 4)
}

func TestRunRefactorLayered_AllUnwindsEveryFeature(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, true)
	stubGoCommands(t, nil)

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
	require.NoError(t, runRefactorLayered(refactorLayeredCmd, nil))

	assert.True(t, fileExistsInFixture(t, "app/services/user.service.go"))
}

// TestRunRefactorLayered_DryRunPlansWithoutWriting mirrors the forward
// dry-run: a plan is printed and nothing on disk changes.
func TestRunRefactorLayered_DryRunPlansWithoutWriting(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	setRefactorFlagsWithAll(t, true /*dry-run*/, false /*all*/)
	before := snapshotTree(t)
	out := captureStdout(t, func() {
		require.NoError(t, runRefactorLayered(refactorLayeredCmd, []string{"User"}))
	})
	after := snapshotTree(t)

	assert.Equal(t, before, after, "dry-run modified the working tree")
	assert.Contains(t, out, "app/user/service.go")
}

func TestRunRefactorLayered_WireFailureAborts(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	stubGoCommands(t, errors.New("wire blew up"))
	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wire generation failed")
}

func TestRunRefactorLayered_UnknownFeatureFails(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	err := runRefactorLayered(refactorLayeredCmd, []string{"Ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/ghost/")
}

func TestMigrateResource_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	// app/user must become a directory; a regular file there blocks it.
	blockPath(t, "app/user")

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir app/user")
}

func TestMigrateResource_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	// The first mapped file for `user` is app/user/dtos.go; a directory there
	// makes the write fail while the parent MkdirAll still succeeds.
	occupyWithDir(t, "app/user/dtos.go")

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/user/dtos.go")
}

func TestRevertResource_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)
	r := userResourceFixture()
	_, _, err := migrateResource(r, fixtureModulePath)
	require.NoError(t, err)

	occupyWithDir(t, "app/dtos/user.dtos.go")

	_, _, err = revertResource(r, fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/dtos/user.dtos.go")
}

func TestRevertResource_MockWriteFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	r := userResourceFixture()

	mockPath := filepath.Join("testutil", "mocks", "user_service_mock.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mockPath), 0o755))
	require.NoError(t, os.WriteFile(mockPath, []byte("package mocks\n\nfunc Broken( {\n"), 0o644))

	_, _, err := revertResource(r, fixtureModulePath)
	require.Error(t, err, "an unparseable mock must abort rather than be written back mangled")
	assert.Contains(t, err.Error(), "featurize reverse")
}

func TestApplySharedRelocations_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	blockPath(t, "app/shared")

	_, err := applySharedRelocations()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir app/shared/dtos")
}

func TestApplySharedRelocations_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	occupyWithDir(t, "app/shared/dtos/aliases.go")

	_, err := applySharedRelocations()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/shared/dtos/aliases.go")
}

func TestApplySharedRelocationsReverse_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	_, err := applySharedRelocations()
	require.NoError(t, err)

	occupyWithDir(t, "app/dtos/aliases.go")

	_, err = applySharedRelocationsReverse()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing app/dtos/aliases.go")
}

func TestRevertPasswordGenerator_WriteFailureIsReported(t *testing.T) {
	inRenderedProject(t)
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
	inRenderedProject(t)
	require.NoError(t, os.MkdirAll("app/user", 0o755))
	require.NoError(t, os.WriteFile("app/user/password_generator.go",
		[]byte("package user\n\nfunc Broken( {\n"), 0o644))

	_, err := revertPasswordGenerator()
	require.Error(t, err)
}

func TestApplyCrossCuttingPatches_WriteFailureIsReported(t *testing.T) {
	requireNonRoot(t)
	inRenderedProject(t)
	makeReadOnly(t, "app/di/container.go")

	_, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/di/container.go")
}

func TestApplyCrossCuttingPatchesReverse_WriteFailureIsReported(t *testing.T) {
	requireNonRoot(t)
	inRenderedProject(t)
	resources := []featurize.Resource{userResourceFixture()}
	_, err := applyCrossCuttingPatches(fixtureModulePath, resources)
	require.NoError(t, err)

	makeReadOnly(t, "app/di/container.go")

	_, err = applyCrossCuttingPatchesReverse(fixtureModulePath, resources)
	require.Error(t, err)
}

func TestFlipLayoutInConfig_ReadFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove("config.yaml"))

	require.Error(t, flipLayoutInConfig())
}

func TestFlipLayoutInConfigReverse_ReadFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove("config.yaml"))

	require.Error(t, flipLayoutInConfigReverse())
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

// TestRunRefactorStatus_TextRendererListsResources covers the human-readable
// renderer. Every other status test asserts on the JSON payload, which leaves
// the text callback — what a developer actually sees — unexercised.
func TestRunRefactorStatus_TextRendererListsResources(t *testing.T) {
	inRenderedProject(t)

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})

	assert.Contains(t, out, "layered")
	assert.Contains(t, out, "Migration target")
	assert.Contains(t, out, "User", "the discovered resources must be listed")
}

// TestRunRefactorStatus_FeatureProjectTextOutput covers the same renderer for a
// migrated project, and with it the layout branch that reports "feature".
func TestRunRefactorStatus_FeatureProjectTextOutput(t *testing.T) {
	inRenderedProject(t)
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)
	require.NoError(t, flipLayoutInConfig())

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})

	assert.Contains(t, out, "feature")
}

// TestRunRefactorStatus_LayeredByFilesystemSignal covers the fallback arm:
// config.yaml carries no layout key, so the layered signal (app/models/) is
// what decides.
func TestRunRefactorStatus_LayeredByFilesystemSignal(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("config.yaml", []byte("server:\n  port: \"8080\"\n"), 0o644))

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})

	assert.Contains(t, out, "layered")
}

// TestRunRefactorFeature_IneligibleProjectStops covers the precondition arm.
func TestRunRefactorFeature_IneligibleProjectStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	require.NoError(t, flipLayoutInConfig()) // already feature

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already feature-package")
}

// TestRunRefactorFeature_DirtyTreeStopsWithoutForce covers the guard arm when
// --force is NOT passed, inside a real git repo.
func TestRunRefactorFeature_DirtyTreeStopsWithoutForce(t *testing.T) {
	inRenderedProject(t)
	gitInitOrSkip(t)
	setRefactorFlagsWithoutForce(t)

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted changes")
}

// TestRunRefactorFeature_MigrateFailureStops: a broken per-resource
// source file is now caught by the eligibility preflight BEFORE any
// write — the historical mid-flight abort left a half-rewritten tree.
func TestRunRefactorFeature_MigrateFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, os.WriteFile("app/services/user.service.go",
		[]byte("package services\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocking condition")
	assert.False(t, fileExistsInFixture(t, "app/user/service.go"),
		"the preflight must refuse before any file moves")
}

// TestRunRefactorFeature_SharedRelocationFailureStops covers the relocation
// step's error arm.
func TestRunRefactorFeature_SharedRelocationFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	blockPath(t, "app/shared")

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/shared")
}

// TestRunRefactorFeature_CrossCuttingFailureStops: a broken
// cross-cutting file is caught by the preflight before any write.
func TestRunRefactorFeature_CrossCuttingFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, os.WriteFile("app/di/container.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocking condition")
	assert.False(t, fileExistsInFixture(t, "app/user/service.go"),
		"the preflight must refuse before any file moves")
}

// TestRunRefactorFeature_ConfigFlipFailureStops covers the config arm.
func TestRunRefactorFeature_ConfigFlipFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, os.Remove("config.yaml"))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
}

// TestRunRefactorFeature_DryRunSkipsAbsentFiles covers the dry-run preview's
// existence check: the plan must list only files that are actually there.
func TestRunRefactorFeature_DryRunSkipsAbsentFiles(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, true /*dry-run*/, false)
	require.NoError(t, os.Remove("app/services/user.service.go"))

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))
	})

	assert.NotContains(t, out, "app/services/user.service.go →",
		"a file that is not on disk must not appear in the plan")
}

func TestRunRefactorLayered_DirtyTreeStopsWithoutForce(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	gitInitOrSkip(t)
	setRefactorFlagsWithoutForce(t)

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted changes")
}

// TestRunRefactorLayered_RevertFailureStops: a broken feature-side file
// is caught by the preflight before the reverse migration writes.
func TestRunRefactorLayered_RevertFailureStops(t *testing.T) {
	migratedProject(t)
	require.NoError(t, os.WriteFile("app/user/service.go",
		[]byte("package user\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocking condition")
	assert.False(t, fileExistsInFixture(t, "app/services/user.service.go"),
		"the preflight must refuse before any file moves back")
}

func TestRunRefactorLayered_SharedRelocationFailureStops(t *testing.T) {
	migratedProject(t)
	occupyWithDir(t, "app/dtos/aliases.go")

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
}

func TestRunRefactorLayered_CrossCuttingFailureStops(t *testing.T) {
	migratedProject(t)
	require.NoError(t, os.WriteFile("app/di/container.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
}

func TestRunRefactorLayered_ConfigFlipFailureStops(t *testing.T) {
	migratedProject(t)
	require.NoError(t, os.Remove("config.yaml"))

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
}

func TestRunRefactorLayered_BuildFailureAborts(t *testing.T) {
	migratedProject(t)

	orig := runGoCommandFn
	t.Cleanup(func() { runGoCommandFn = orig })
	runGoCommandFn = func(args ...string) error {
		if len(args) > 0 && args[0] == "build" {
			return errors.New("build blew up")
		}
		return nil
	}

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go build failed")
}

// TestRequireCleanGitTree_NoRepoIsPermissive covers the permissive return: a
// project that is not under git has no dirty state to guard against, and the
// refactor must not refuse to run.
func TestRequireCleanGitTree_NoRepoIsPermissive(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	inRenderedProject(t)

	assert.NoError(t, requireCleanGitTree(),
		"outside a git repo the guard must not block the refactor")
}

func TestRevertPasswordGenerator_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.MkdirAll("app/user", 0o755))
	require.NoError(t, os.WriteFile("app/user/password_generator.go",
		[]byte("package user\n\ntype PasswordGenerator struct{}\n"), 0o644))
	blockPath(t, "app/services")

	_, err := revertPasswordGenerator()
	require.Error(t, err)
}

func TestApplyCrossCuttingPatchesReverse_TransformFailureIsReported(t *testing.T) {
	migratedProject(t)
	require.NoError(t, os.WriteFile("app/di/wire.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	_, err := applyCrossCuttingPatchesReverse(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.Error(t, err)
}

// TestFlipLayoutInConfig_AlreadyFeatureIsIdempotent covers the arm where the
// key is already set to the target value.
func TestFlipLayoutInConfig_AlreadyFeatureIsIdempotent(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, flipLayoutInConfig())

	require.NoError(t, flipLayoutInConfig())
	assert.Contains(t, readFixtureFile(t, "config.yaml"), "layout: feature")
}

// TestFlipLayoutInConfig_NoLayoutKeyAppendsOne covers the default arm: a
// config.yaml written before the layout key existed must gain one rather than
// be left silently unflagged.
func TestFlipLayoutInConfig_NoLayoutKeyAppendsOne(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("config.yaml", []byte("server:\n  port: \"8080\"\n"), 0o644))

	require.NoError(t, flipLayoutInConfig())
	assert.Contains(t, readFixtureFile(t, "config.yaml"), "feature")
}

// TestFlipLayoutInConfigReverse_NoLayoutKeyIsANoOp documents an asymmetry
// between the two directions.
//
// Forward PREPENDS a project block when config.yaml has no layout key, because
// a project scaffolded before the key existed still needs flagging. Reverse is
// a plain replace and leaves such a file untouched — which is correct in
// context, since reverse only ever runs on a project that went forward and
// therefore already carries the key.
func TestFlipLayoutInConfigReverse_NoLayoutKeyIsANoOp(t *testing.T) {
	inRenderedProject(t)
	const noLayout = "server:\n  port: \"8080\"\n"
	require.NoError(t, os.WriteFile("config.yaml", []byte(noLayout), 0o644))

	require.NoError(t, flipLayoutInConfigReverse())
	assert.Equal(t, noLayout, readFixtureFile(t, "config.yaml"),
		"with no layout key there is nothing to flip back")
}

// TestToPascalCaseSimple_SkipsEmptySegments covers the empty-part guard, which
// a name with doubled or trailing separators produces.
func TestToPascalCaseSimple_SkipsEmptySegments(t *testing.T) {
	assert.Equal(t, "PurchaseOrder", toPascalCaseSimple("purchase__order"))
	assert.Equal(t, "PurchaseOrder", toPascalCaseSimple("purchase-order-"))
	assert.Equal(t, "PurchaseOrder", toPascalCaseSimple("-purchase_order"))
	assert.Empty(t, toPascalCaseSimple("___"))
}

// TestPruneEmptyFeatureDirs_MissingDirIsSkipped covers the read-error arm: a
// feature directory that is already gone is nothing to prune.
func TestPruneEmptyFeatureDirs_MissingDirIsSkipped(t *testing.T) {
	inRenderedProject(t)

	assert.NotPanics(t, func() {
		pruneEmptyFeatureDirs([]featurize.Resource{
			{Name: "Ghost", Snake: "ghost", Plural: "Ghosts"},
		})
	})
}

// TestPruneEmptyLayeredDirs_MissingDirsAreSkipped covers the same arm on the
// forward side.
func TestPruneEmptyLayeredDirs_MissingDirsAreSkipped(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.RemoveAll("app"))

	assert.NotPanics(t, pruneEmptyLayeredDirs)
}

// TestApplyCrossCuttingPatches_SkipsAbsentDtosConsumers covers the read-error
// continue in the dtos-import loop. Those three files are optional — a
// REST-only project has no resolver — so a missing one is not an error.
func TestApplyCrossCuttingPatches_SkipsAbsentDtosConsumers(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove("app/validators/app_validator.go"))

	patched, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.NoError(t, err)
	assert.NotContains(t, patched, "app/validators/app_validator.go")
}

func TestApplyCrossCuttingPatches_DtosConsumerTransformFailure(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("app/validators/app_validator.go",
		[]byte("package validators\n\nfunc Broken( {\n"), 0o644))

	_, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fix dtos import")
}

func TestApplyCrossCuttingPatches_DtosConsumerWriteFailure(t *testing.T) {
	requireNonRoot(t)
	inRenderedProject(t)
	makeReadOnly(t, "app/validators/app_validator.go")

	_, err := applyCrossCuttingPatches(fixtureModulePath, []featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/validators/app_validator.go")
}

func TestApplyCrossCuttingPatchesReverse_SkipsAbsentJobs(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove("app/di/wire.go"))
	require.NoError(t, os.Remove("app/validators/app_validator.go"))

	patched, err := applyCrossCuttingPatchesReverse(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.NoError(t, err)
	assert.NotContains(t, patched, "app/di/wire.go")
}

func TestApplyCrossCuttingPatchesReverse_DtosConsumerTransformFailure(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("app/validators/app_validator.go",
		[]byte("package validators\n\nfunc Broken( {\n"), 0o644))

	_, err := applyCrossCuttingPatchesReverse(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fix shared dtos import reverse")
}

func TestApplyCrossCuttingPatchesReverse_DtosConsumerWriteFailure(t *testing.T) {
	requireNonRoot(t)
	inRenderedProject(t)
	makeReadOnly(t, "app/validators/app_validator.go")

	_, err := applyCrossCuttingPatchesReverse(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.Error(t, err)
}

// TestApplySharedRelocationsReverse_MkdirFailureIsReported covers the mkdir arm
// of the reverse relocation.
func TestApplySharedRelocationsReverse_MkdirFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	_, err := applySharedRelocations()
	require.NoError(t, err)
	blockPath(t, "app/dtos")

	_, err = applySharedRelocationsReverse()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir app/dtos")
}

// TestMigrateResource_MockTransformFailureIsReported covers the mock transform
// arm: an unparseable mock must abort rather than be written back mangled.
func TestMigrateResource_MockTransformFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	mockPath := filepath.Join("testutil", "mocks", "user_service_mock.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mockPath), 0o755))
	require.NoError(t, os.WriteFile(mockPath, []byte("package mocks\n\nfunc Broken( {\n"), 0o644))

	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "featurize")
}

// TestRelocatePasswordGenerator_AbsentFileIsANoOp covers the read-error return:
// a project that already migrated has nothing left to move.
func TestRelocatePasswordGenerator_AbsentFileIsANoOp(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove("app/services/password_generator.go"))

	moved, err := relocatePasswordGenerator(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.NoError(t, err)
	assert.Empty(t, moved)
}

func TestRelocatePasswordGenerator_TransformFailureIsReported(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile("app/services/password_generator.go",
		[]byte("package services\n\nfunc Broken( {\n"), 0o644))

	_, err := relocatePasswordGenerator(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password_generator.go")
}

// TestRequireCleanGitTree_CleanRepoPasses covers the success return. An empty
// repo reports a clean tree without needing a commit.
func TestRequireCleanGitTree_CleanRepoPasses(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, exec.Command("git", "init").Run())

	assert.NoError(t, requireCleanGitTree(), "an empty repo has no uncommitted changes")
}

func TestRunRefactorFeature_ResolveFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, true /*all*/)
	stubGoCommands(t, nil)
	require.NoError(t, os.RemoveAll("app/models"))
	require.NoError(t, os.MkdirAll("app/models", 0o755))

	err := runRefactorFeature(refactorFeatureCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no layered resources")
}

func TestRunRefactorLayered_ResolveFailureStops(t *testing.T) {
	migratedProject(t)
	setRefactorFlagsWithAll(t, false, true /*all*/)
	require.NoError(t, os.RemoveAll("app/user"))

	err := runRefactorLayered(refactorLayeredCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no feature directories")
}

func TestRunRefactorLayered_PasswordGeneratorFailureStops(t *testing.T) {
	migratedProject(t)
	require.NoError(t, os.WriteFile("app/user/password_generator.go",
		[]byte("package user\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
}

// TestApplyCrossCuttingPatches_SkipsAbsentJobs covers the read-error continue
// in the cross-cutting jobs loop (the dtos loop above it is a separate list).
// A REST-only project has no GraphQL resolver, so a missing target is normal.
func TestApplyCrossCuttingPatches_SkipsAbsentJobs(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove("app/di/wire.go"))

	patched, err := applyCrossCuttingPatches(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.NoError(t, err)
	assert.NotContains(t, patched, "app/di/wire.go")
}

// TestRunRefactorFeature_UnreadableModulePathStops and its layered twin cover
// the readModulePath arm: without a module path the transforms cannot rewrite
// import paths, so the refactor must stop before touching anything.
func TestRunRefactorFeature_UnreadableModulePathStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, os.Remove("go.mod"))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go.mod")
}

func TestRunRefactorLayered_UnreadableModulePathStops(t *testing.T) {
	migratedProject(t)
	require.NoError(t, os.Remove("go.mod"))

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go.mod")
}

// TestRunRefactorFeature_PasswordGeneratorFailureStops: a broken
// password_generator.go is caught by the preflight before any write —
// the file is on the transformed set (it follows the User feature).
func TestRunRefactorFeature_PasswordGeneratorFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, os.WriteFile("app/services/password_generator.go",
		[]byte("package services\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocking condition")
	assert.False(t, fileExistsInFixture(t, "app/user/password_generator.go"),
		"the preflight must refuse before the relocation")
}

// TestRunRefactorLayered_ConfigFlipWriteFailureStops covers the reverse config
// flip's error arm. config.yaml must stay READABLE — the eligibility check
// reads it — while being unwritable, so this needs permissions.
func TestRunRefactorLayered_ConfigFlipWriteFailureStops(t *testing.T) {
	requireNonRoot(t)
	migratedProject(t)
	makeReadOnly(t, "config.yaml")

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
}

// fileExistsInFixture reports whether path (relative to the project root)
// exists.
func fileExistsInFixture(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// readFixtureFile returns the contents of a file relative to the project root.
func readFixtureFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

// stubGoCommands swaps the wire/build seam for the duration of a test and
// records the invocations, so a test can assert the orchestrator asked for the
// right steps in the right order.
func stubGoCommands(t *testing.T, err error) *[][]string {
	t.Helper()
	var calls [][]string
	orig := runGoCommandFn
	runGoCommandFn = func(args ...string) error {
		calls = append(calls, args)
		return err
	}
	t.Cleanup(func() { runGoCommandFn = orig })
	return &calls
}

// setRefactorFlagsWithAll is setRefactorFlags with control over --all. The
// shared helper hardcodes all=true, which makes the orchestrators ignore their
// positional argument — these tests need both paths.
//
// --force is always on here: every test in this file runs inside a temp dir
// that is not a git repo, so the dirty-tree guard is irrelevant to what they
// exercise. refactor_test.go covers that guard directly, in both directions.
func setRefactorFlagsWithAll(t *testing.T, dryRun, all bool) {
	const force = true
	t.Helper()
	b := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	for name, value := range map[string]bool{"all": all, "dry-run": dryRun, "force": force} {
		require.NoError(t, refactorFeatureCmd.Flags().Set(name, b(value)))
		require.NoError(t, refactorLayeredCmd.Flags().Set(name, b(value)))
	}
	t.Cleanup(func() {
		for _, n := range []string{"all", "dry-run", "force"} {
			_ = refactorFeatureCmd.Flags().Set(n, "false")
			_ = refactorLayeredCmd.Flags().Set(n, "false")
		}
	})
}

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

func requireNonRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the file mode this test depends on")
	}
}

// makeReadOnly leaves the file readable so the read succeeds, then removes
// write permission so os.WriteFile fails.
func makeReadOnly(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Chmod(path, 0o444))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
}

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

// migratedProject renders a project and migrates it forward, leaving the
// caller inside a feature-layout tree ready to be unwound.
func migratedProject(t *testing.T) {
	t.Helper()
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))
}

// setRefactorFlagsWithoutForce turns --force off while leaving the other flags
// as the previous call left them, so the dirty-tree guard actually runs.
func setRefactorFlagsWithoutForce(t *testing.T) {
	t.Helper()
	require.NoError(t, refactorFeatureCmd.Flags().Set("force", "false"))
	require.NoError(t, refactorLayeredCmd.Flags().Set("force", "false"))
	t.Cleanup(func() {
		_ = refactorFeatureCmd.Flags().Set("force", "false")
		_ = refactorLayeredCmd.Flags().Set("force", "false")
	})
}

// ---------- GraphQL phase ----------
//
// A GraphQL project's resolver files never move, but their imports and
// selectors must follow the migrated symbols, gqlgen.yml must follow
// the dtos relocation, and gqlgen must be re-run between the config
// flip and wire. These tests render the --graphql skeleton variant.

// migratedGraphQLProject renders a GraphQL project and migrates it
// forward, leaving the caller inside a feature-layout GraphQL tree.
func migratedGraphQLProject(t *testing.T) {
	t.Helper()
	inRenderedGraphQLProject(t)
	setRefactorFlagsWithAll(t, false, true)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
}

func TestRunRefactorFeature_GraphQLProjectPatchesResolverFiles(t *testing.T) {
	inRenderedGraphQLProject(t)
	setRefactorFlagsWithAll(t, false, true)

	// Seed a stale layered-shaped wire_gen.go: gqlgen's validation pass
	// compiles the module, so the stale file must have been deleted (by
	// the wire step, which runs first) before the gqlgen call fires.
	require.NoError(t, os.WriteFile("app/di/wire_gen.go",
		[]byte("package di\n\nimport _ \""+fixtureModulePath+"/app/repositories\"\n"), 0o644))

	var calls [][]string
	orig := runGoCommandFn
	runGoCommandFn = func(args ...string) error {
		calls = append(calls, args)
		if len(args) > 1 && args[1] == "gqlgen" {
			assert.False(t, fileExistsInFixture(t, "app/di/wire_gen.go"),
				"stale wire_gen.go must be gone (deleted by the wire step) before gqlgen runs")
			assert.False(t, fileExistsInFixture(t, "app/generated.go"),
				"stale exec file must be deleted before gqlgen runs")
		}
		return nil
	}
	t.Cleanup(func() { runGoCommandFn = orig })

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))

	// user.resolvers.go: per-resource symbols re-qualified, services
	// import gone, shared dtos import flipped. The file did NOT move.
	resolvers := readFixtureFile(t, "app/graphql/resolvers/user.resolvers.go")
	assert.Contains(t, resolvers, "userpkg.TCreateUserDto")
	assert.Contains(t, resolvers, "userpkg.ErrUserNotFound")
	assert.Contains(t, resolvers, "dtos.TPaginationObjectDto",
		"shared aliases keep the dtos qualifier")
	assert.Contains(t, resolvers, fixtureModulePath+"/app/shared/dtos")
	assert.NotContains(t, resolvers, `"`+fixtureModulePath+`/app/services"`)

	// resolver.go: the DI struct follows the service interface.
	resolverStruct := readFixtureFile(t, "app/graphql/resolvers/resolver.go")
	assert.Contains(t, resolverStruct, "userpkg.UserServiceInterface")
	assert.NotContains(t, resolverStruct, "svcInterfaces")

	// gql_filters.go: generated dtos type keeps its qualifier, the
	// domain filter follows the feature package.
	filters := readFixtureFile(t, "app/graphql/resolvers/gql_filters.go")
	assert.Contains(t, filters, "dtos.UserFiltersDto")
	assert.Contains(t, filters, "userpkg.ListUsersFilter")

	// gqlgen.yml rewritten: model path + autobind expansion.
	gqlgenYaml := readFixtureFile(t, "gqlgen.yml")
	assert.Contains(t, gqlgenYaml, "app/shared/dtos/generated-types.dtos.go")
	assert.Contains(t, gqlgenYaml, `- "`+fixtureModulePath+`/app/shared/dtos"`)
	assert.Contains(t, gqlgenYaml, `- "`+fixtureModulePath+`/app/user"`)
	assert.NotContains(t, gqlgenYaml, `- "`+fixtureModulePath+`/app/dtos"`)

	// The generated models file relocated with the shared dtos.
	assert.True(t, fileExistsInFixture(t, "app/shared/dtos/generated-types.dtos.go"))
	assert.False(t, fileExistsInFixture(t, "app/dtos/generated-types.dtos.go"))

	// The stale exec file is deleted before gqlgen reruns.
	assert.False(t, fileExistsInFixture(t, "app/generated.go"))

	// Pipeline order: wire → gqlgen → build. gqlgen's validation pass
	// compiles the module, which needs the regenerated wire_gen.go.
	require.Len(t, calls, 3)
	assert.Equal(t, []string{"tool", "wire", "./app/di/"}, calls[0])
	assert.Equal(t, []string{"tool", "gqlgen", "generate"}, calls[1])
	assert.Equal(t, []string{"build", "./..."}, calls[2])
}

func TestRunRefactorFeature_GraphQLGqlgenFailureAborts(t *testing.T) {
	inRenderedGraphQLProject(t)
	setRefactorFlagsWithAll(t, false, true)
	orig := runGoCommandFn
	t.Cleanup(func() { runGoCommandFn = orig })
	runGoCommandFn = func(args ...string) error {
		if len(args) > 1 && args[1] == "gqlgen" {
			return errors.New("gqlgen blew up")
		}
		return nil
	}

	err := runRefactorFeature(refactorFeatureCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gqlgen generation failed")
	assert.Contains(t, err.Error(), "git restore")
}

func TestApplyGraphQLPatches_SkipNoticeForRestOnlyResource(t *testing.T) {
	inRenderedGraphQLProject(t)

	invoice := featurize.Resource{Name: "Invoice", Snake: "invoice", Plural: "Invoices"}
	patched, skipped, err := applyGraphQLPatches(fixtureModulePath,
		[]featurize.Resource{userResourceFixture(), invoice})
	require.NoError(t, err)

	// Every resolver file plus gqlgen.yml is patched.
	assert.Contains(t, patched, "app/graphql/resolvers/user.resolvers.go")
	assert.Contains(t, patched, "app/graphql/resolvers/resolver.go")
	assert.Contains(t, patched, "gqlgen.yml")
	// Invoice has no resolver file — visible skip, not an error.
	assert.Equal(t, []string{"app/graphql/resolvers/invoice.resolvers.go"}, skipped)
}

func TestApplyGraphQLPatches_RestOnlyProjectIsNoop(t *testing.T) {
	inRenderedProject(t) // REST-only: no app/graphql/, no gqlgen.yml
	assert.False(t, hasGraphQLArtifacts())

	patched, skipped, err := applyGraphQLPatches(fixtureModulePath,
		[]featurize.Resource{userResourceFixture()})
	require.NoError(t, err)
	assert.Empty(t, patched)
	// The skip list reports the missing resolver, but the orchestrator
	// never calls this function for REST-only projects (hasGraphQLArtifacts
	// gates the phase).
	assert.NotEmpty(t, skipped)
}

func TestRunRefactorFeature_RestOnlyProjectNeverRunsGqlgen(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	calls := stubGoCommands(t, nil)

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))
	})

	for _, c := range *calls {
		assert.NotContains(t, c, "gqlgen", "REST-only refactor must not invoke gqlgen")
	}
	assert.NotContains(t, out, "GraphQL", "REST-only refactor must not emit GraphQL output")
}

func TestRunRefactorFeature_GraphQLDryRunListsPlan(t *testing.T) {
	inRenderedGraphQLProject(t)
	setRefactorFlagsWithAll(t, true /*dry-run*/, true)

	before := snapshotTree(t)
	out := captureStdout(t, func() {
		require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
	})
	after := snapshotTree(t)

	assert.Contains(t, out, "patch: app/graphql/resolvers/user.resolvers.go")
	assert.Contains(t, out, "patch: app/graphql/resolvers/resolver.go")
	assert.Contains(t, out, "rewrite: gqlgen.yml")
	assert.Contains(t, out, "Would run: go tool gqlgen generate")
	// The generated-types move is part of the shared relocations.
	assert.Contains(t, out, "app/dtos/generated-types.dtos.go → app/shared/dtos/generated-types.dtos.go")
	assert.Equal(t, before, after, "dry-run modified the working tree")
}

func TestRunRefactorLayered_GraphQLRoundTripRestoresLayeredShape(t *testing.T) {
	migratedGraphQLProject(t)
	setRefactorFlagsWithAll(t, false, true)
	calls := stubGoCommands(t, nil)

	require.NoError(t, runRefactorLayered(refactorLayeredCmd, nil))

	// Resolver files back to the layered shape.
	resolvers := readFixtureFile(t, "app/graphql/resolvers/user.resolvers.go")
	assert.Contains(t, resolvers, "services.ErrUserNotFound")
	assert.Contains(t, resolvers, "dtos.TCreateUserDto")
	assert.Contains(t, resolvers, `"`+fixtureModulePath+`/app/dtos"`)
	assert.NotContains(t, resolvers, "userpkg")
	assert.NotContains(t, resolvers, "app/shared/dtos")

	resolverStruct := readFixtureFile(t, "app/graphql/resolvers/resolver.go")
	assert.Contains(t, resolverStruct, "svcInterfaces.UserServiceInterface")

	// gqlgen.yml restored byte-for-byte to the layered shape.
	gqlgenYaml := readFixtureFile(t, "gqlgen.yml")
	assert.Contains(t, gqlgenYaml, "filename: app/dtos/generated-types.dtos.go")
	assert.Contains(t, gqlgenYaml, `- "`+fixtureModulePath+`/app/dtos"`)
	assert.NotContains(t, gqlgenYaml, "shared/dtos")
	assert.NotContains(t, gqlgenYaml, `- "`+fixtureModulePath+`/app/user"`)

	// The generated models file moved back.
	assert.True(t, fileExistsInFixture(t, "app/dtos/generated-types.dtos.go"))
	assert.False(t, fileExistsInFixture(t, "app/shared/dtos/generated-types.dtos.go"))

	// Reverse pipeline order matches forward: wire → gqlgen → build.
	require.Len(t, *calls, 3)
	assert.Equal(t, []string{"tool", "wire", "./app/di/"}, (*calls)[0])
	assert.Equal(t, []string{"tool", "gqlgen", "generate"}, (*calls)[1])
	assert.Equal(t, []string{"build", "./..."}, (*calls)[2])
}

func TestWarnPartialGraphQLMigration(t *testing.T) {
	inRenderedGraphQLProject(t)

	all := []featurize.Resource{userResourceFixture(), {Name: "Invoice", Snake: "invoice", Plural: "Invoices"}}
	discoverErr := error(nil)
	discover := func() ([]featurize.Resource, error) { return all, discoverErr }

	out := captureStdout(t, func() {
		warnPartialGraphQLMigration(all[:1], discover)
	})
	assert.Contains(t, out, "migrate all resources together")

	out = captureStdout(t, func() {
		warnPartialGraphQLMigration(all, discover)
	})
	assert.NotContains(t, out, "migrate all resources together",
		"full-scope migrations must not warn")

	// A discovery failure must stay silent — the warning is advisory,
	// and the migration itself will surface the real error.
	discoverErr = errors.New("discovery blew up")
	out = captureStdout(t, func() {
		warnPartialGraphQLMigration(all[:1], discover)
	})
	assert.NotContains(t, out, "migrate all resources together")
}

// ---------- Eligibility preflight wiring ----------

// TestRunRefactorFeature_NoGitRefusedWithoutForce: without --force and
// without a git repository the migration refuses — there is no
// recovery path for an aborted run. (requireCleanGitTree is permissive
// when there's no repo, so the preflight owns this gate.)
func TestRunRefactorFeature_NoGitRefusedWithoutForce(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	setRefactorFlagsWithoutForce(t)
	stubGoCommands(t, nil)

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
	assert.False(t, fileExistsInFixture(t, "app/user/service.go"),
		"refusal must happen before any write")
}

// TestRunRefactorFeature_TornStateRefusedEvenWithForce: --force only
// bypasses the dirty-tree/no-git safety net — a torn per-resource
// state is a hard refusal.
func TestRunRefactorFeature_TornStateRefusedEvenWithForce(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false) // force=true via the helper
	stubGoCommands(t, nil)
	writeRefactorFile(t, "app/user/service.go", "package user\n")

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocking condition")
	assert.True(t, fileExistsInFixture(t, "app/services/user.service.go"),
		"no layered file may move when the preflight refuses")
}

// TestRunRefactorFeature_DryRunShowsFindingsWithoutRefusing: dry-run
// writes nothing, so it reports the findings instead of refusing.
func TestRunRefactorFeature_DryRunShowsFindingsWithoutRefusing(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, true /*dry-run*/, false)
	writeRefactorFile(t, "app/user/service.go", "package user\n")

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))
	})
	assert.Contains(t, out, "[layout-state]")
	assert.Contains(t, out, "BOTH layouts")
}

// TestRunRefactorStatus_ReportsEligibility: status surfaces the same
// preflight — blockers and warnings — while staying exit-zero.
func TestRunRefactorStatus_ReportsEligibility(t *testing.T) {
	inRenderedProject(t)
	writeRefactorFile(t, "app/user/service.go", "package user\n")

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})
	assert.Contains(t, out, "Eligibility:      ✗")
	assert.Contains(t, out, "[layout-state]")
}

func TestRunRefactorStatus_EligibleProject(t *testing.T) {
	inRenderedProject(t)
	gitInitOrSkip(t)

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})
	assert.Contains(t, out, "Eligibility:      ✓ eligible")
}

func TestRunRefactorStatus_JSONCarriesPreflight(t *testing.T) {
	inRenderedProject(t)
	withJSONMode(t)

	out := captureStdout(t, func() {
		require.NoError(t, runRefactorStatus(refactorStatusCmd, nil))
	})
	var res refactorStatusResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.NotNil(t, res.Preflight)
	// The temp fixture is not a git repo — the verdict must say so.
	require.Len(t, res.Preflight.Blockers, 1)
	assert.Equal(t, "no-git", res.Preflight.Blockers[0].Check)
}

func TestPreflightRefusal_CodeSelection(t *testing.T) {
	var onlyGit preflightReport
	onlyGit.block("no-git", "", "no repo")
	err := preflightRefusal(&onlyGit)
	assert.Contains(t, err.Error(), "not a git repository")
	assert.Contains(t, err.Error(), "--force")

	var mixed preflightReport
	mixed.block("no-git", "", "no repo")
	mixed.block("layout-state", "app/user", "torn")
	err = preflightRefusal(&mixed)
	assert.Contains(t, err.Error(), "2 blocking condition(s)")
}
