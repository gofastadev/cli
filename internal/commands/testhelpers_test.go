// Shared fixtures for the internal/commands test suite.
//
// renderSkeleton/inRenderedProject build a real project tree from the embedded
// skeleton; the blockPath/occupyWithDir/lockDir/makeReadOnly helpers induce
// filesystem failures. Used by the refactor, routes, verify, init_cmd and new
// tests, which is why they live here rather than in any one of them.

package commands

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/gofastadev/cli/internal/skeleton"
	"github.com/stretchr/testify/require"
)

// fixtureModulePath is the module path every rendered fixture declares.
const fixtureModulePath = "example.com/fixtureapp"

// renderSkeleton writes the embedded skeleton into dir, mirroring runNew's
// walk: dotfile renames, .tmpl rendering, and the GraphQL-only skip list.
//
// It renders the layered, non-GraphQL variant — the only shape the refactor
// tests need, since the refactor's job is to turn exactly that into a feature
// project. Add the knobs back when a test needs a different one.
func renderSkeleton(t *testing.T, dir string) {
	t.Helper()
	const layout, graphQL = "layered", false

	data := ProjectData{
		ProjectName:      "Fixtureapp",
		ProjectNameLower: "fixtureapp",
		ProjectNameUpper: "FIXTUREAPP",
		ModulePath:       fixtureModulePath,
		GraphQL:          graphQL,
		DBDriver:         "postgres",
		Layout:           layout,
		JWTSecret:        "fixture-jwt-secret",
		SessionSecret:    "fixture-session-secret",
	}

	require.NoError(t, fs.WalkDir(skeleton.ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "project/")
		if rel == "" || rel == "project" {
			return nil
		}
		if !graphQL {
			for _, prefix := range graphqlOnlyPaths {
				if strings.HasPrefix(rel, prefix) {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
			}
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}

		content, err := fs.ReadFile(skeleton.ProjectFS, path)
		if err != nil {
			return err
		}

		out := rel
		isTemplate := strings.HasSuffix(out, ".tmpl")
		if isTemplate {
			out = strings.TrimSuffix(out, ".tmpl")
		}
		if renamed, ok := dotfileRenames[filepath.Base(out)]; ok {
			out = filepath.Join(filepath.Dir(out), renamed)
		}

		body := content
		if isTemplate {
			tmpl, terr := template.New(filepath.Base(path)).Parse(string(content))
			if terr != nil {
				return terr
			}
			var buf strings.Builder
			if eerr := tmpl.Execute(&buf, data); eerr != nil {
				return eerr
			}
			body = []byte(buf.String())
		}

		full := filepath.Join(dir, out)
		if mkErr := os.MkdirAll(filepath.Dir(full), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(full, body, 0o644)
	}))

	// go.mod is produced by `go mod init` in production, not by the skeleton.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+fixtureModulePath+"\n\ngo "+scaffoldGoVersion+"\n"), 0o644))
}

// inRenderedProject renders a skeleton into a temp dir and chdirs into it for
// the duration of the test. The refactor functions all operate on paths
// relative to the working directory, so this is how they are addressed.
func inRenderedProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	renderSkeleton(t, dir)

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// userResourceFixture is the resource every scaffold ships with, and therefore
// the one the refactor tests can migrate without generating anything first.
func userResourceFixture() featurize.Resource {
	return featurize.Resource{Name: "User", Snake: "user", Plural: "Users"}
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
