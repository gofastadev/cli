package commands

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// End-to-end coverage for the two refactor orchestrators against a real
// rendered skeleton.
//
// Both finish by regenerating Wire and running `go build ./...`, which needs a
// fully resolved module — minutes of work and a network round-trip that says
// nothing about the orchestration itself. Those two steps are stubbed through
// runGoCommandFn; everything before them (preconditions, resolution, the file
// moves, the cross-cutting patches, pruning, the config flip) runs for real
// against real scaffold files.

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
func setRefactorFlagsWithAll(t *testing.T, dryRun, force, all bool) {
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

// --- runRefactorFeature, full run ---

// TestRunRefactorFeature_MigratesTheWholeProject is the forward orchestrator's
// happy path: after it returns, the project is in feature shape on disk and
// config.yaml says so.
func TestRunRefactorFeature_MigratesTheWholeProject(t *testing.T) {
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false /*dry-run*/, true /*force*/, false /*all*/)
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
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, true)
	stubGoCommands(t, nil)

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
	assert.True(t, fileExistsInFixture(t, "app/user/service.go"))
}

// TestRunRefactorFeature_WireFailureAborts covers the abort path. The message
// has to point at recovery, because the tree is already half-rewritten.
func TestRunRefactorFeature_WireFailureAborts(t *testing.T) {
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)
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
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)

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
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)
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
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)
	stubGoCommands(t, nil)

	err := runRefactorFeature(refactorFeatureCmd, []string{"Ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost.model.go")
}

// --- runRefactorLayered, full run ---

// TestRunRefactorLayered_UnwindsTheWholeProject is the reverse orchestrator's
// happy path, run on a project that was migrated forward first.
func TestRunRefactorLayered_UnwindsTheWholeProject(t *testing.T) {
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)
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
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, true)
	stubGoCommands(t, nil)

	require.NoError(t, runRefactorFeature(refactorFeatureCmd, nil))
	require.NoError(t, runRefactorLayered(refactorLayeredCmd, nil))

	assert.True(t, fileExistsInFixture(t, "app/services/user.service.go"))
}

// TestRunRefactorLayered_DryRunPlansWithoutWriting mirrors the forward
// dry-run: a plan is printed and nothing on disk changes.
func TestRunRefactorLayered_DryRunPlansWithoutWriting(t *testing.T) {
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	setRefactorFlagsWithAll(t, true /*dry-run*/, true /*force*/, false /*all*/)
	before := snapshotTree(t)
	out := captureStdout(t, func() {
		require.NoError(t, runRefactorLayered(refactorLayeredCmd, []string{"User"}))
	})
	after := snapshotTree(t)

	assert.Equal(t, before, after, "dry-run modified the working tree")
	assert.Contains(t, out, "app/user/service.go")
}

func TestRunRefactorLayered_WireFailureAborts(t *testing.T) {
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	stubGoCommands(t, errors.New("wire blew up"))
	err := runRefactorLayered(refactorLayeredCmd, []string{"User"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wire generation failed")
}

func TestRunRefactorLayered_UnknownFeatureFails(t *testing.T) {
	inRenderedProject(t, "layered")
	setRefactorFlagsWithAll(t, false, true, false)
	stubGoCommands(t, nil)
	require.NoError(t, runRefactorFeature(refactorFeatureCmd, []string{"User"}))

	err := runRefactorLayered(refactorLayeredCmd, []string{"Ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app/ghost/")
}
