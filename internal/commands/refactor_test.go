package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixture helpers ---

// writeRefactorFile creates parent dirs and writes content at a
// fixture-relative path.
func writeRefactorFile(t *testing.T, rel, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(rel), 0o755))
	require.NoError(t, os.WriteFile(rel, []byte(content), 0o644))
}

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

// --- runRefactorStatus ---

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

// --- runRefactorFeature --dry-run ---

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

// --- dirty-tree guard / --force ---

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

// --- runRefactorLayered not-a-feature-project error path ---

func TestRunRefactorLayered_RejectsLayeredProject(t *testing.T) {
	layeredRefactorFixture(t) // config.layout: layered, so "nothing to unwind"
	setRefactorFlags(t, false, false)

	err := runRefactorLayered(refactorLayeredCmd, nil)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "not in feature-package layout")
}

// --- helper coverage: toPascalCaseSimple splits on '_' AND '-' ---

func TestToPascalCaseSimple_SplitsUnderscoreAndDash(t *testing.T) {
	// Mirrors internal/generate/stringutil.go's toPascalCase so
	// `gofasta refactor` and `gofasta g scaffold` agree on names.
	assert.Equal(t, "OrderItem", toPascalCaseSimple("order_item"))
	assert.Equal(t, "OrderItem", toPascalCaseSimple("order-item"))
	assert.Equal(t, "ApiKeyToken", toPascalCaseSimple("api-key_token"))
}
