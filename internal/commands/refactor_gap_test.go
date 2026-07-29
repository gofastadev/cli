package commands

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Remaining branch coverage for refactor.go: the orchestrators' error
// propagation, the text renderer, and the last few filesystem failures.

// --- runRefactorStatus text output ---

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

// --- runRefactorFeature error propagation ---

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

// TestRunRefactorFeature_MigrateFailureStops covers the per-resource migrate
// error surfacing out of the loop.
func TestRunRefactorFeature_MigrateFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, os.WriteFile("app/services/user.service.go",
		[]byte("package services\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "featurize")
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

// TestRunRefactorFeature_CrossCuttingFailureStops covers the patch step's arm.
func TestRunRefactorFeature_CrossCuttingFailureStops(t *testing.T) {
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, os.WriteFile("app/di/container.go",
		[]byte("package di\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorFeature(refactorFeatureCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transform")
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

// --- runRefactorLayered error propagation ---

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

// migratedProject renders a project and migrates it forward, leaving the
// caller inside a feature-layout tree ready to be unwound.
func migratedProject(t *testing.T) {
	t.Helper()
	inRenderedProject(t)
	setRefactorFlagsWithAll(t, false, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))
}

func TestRunRefactorLayered_RevertFailureStops(t *testing.T) {
	migratedProject(t)
	require.NoError(t, os.WriteFile("app/user/service.go",
		[]byte("package user\n\nfunc Broken( {\n"), 0o644))

	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "featurize reverse")
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

// --- requireCleanGitTree, no repo ---

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

// --- remaining filesystem failures ---

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

// --- flipLayoutInConfig branches ---

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

// --- toPascalCaseSimple ---

// TestToPascalCaseSimple_SkipsEmptySegments covers the empty-part guard, which
// a name with doubled or trailing separators produces.
func TestToPascalCaseSimple_SkipsEmptySegments(t *testing.T) {
	assert.Equal(t, "PurchaseOrder", toPascalCaseSimple("purchase__order"))
	assert.Equal(t, "PurchaseOrder", toPascalCaseSimple("purchase-order-"))
	assert.Equal(t, "PurchaseOrder", toPascalCaseSimple("-purchase_order"))
	assert.Empty(t, toPascalCaseSimple("___"))
}

// --- pruneEmptyFeatureDirs / pruneEmptyLayeredDirs read errors ---

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
