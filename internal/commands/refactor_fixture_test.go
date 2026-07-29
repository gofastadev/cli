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

// Fixture support for the refactor tests.
//
// The refactor command rewrites a project tree in place: it moves per-resource
// files between layered and feature locations, patches the cross-cutting files
// (container, wire, index routes, core providers), and prunes the directories
// it empties. Testing that against a hand-written stub tree proves very little
// — the interesting failures are the ones where a real scaffold's file has a
// shape the transform did not expect.
//
// So these fixtures render the SAME embedded skeleton `gofasta new` ships,
// through the same template data and the same dotfile-rename and
// GraphQL-skipping rules. What is deliberately NOT reproduced is runNew's
// dependency resolution — `go mod init`, `go get …`, `go mod tidy` — which
// needs the network, takes minutes, and has no bearing on how the refactor
// rewrites source.

// fixtureModulePath is the module path every rendered fixture declares.
const fixtureModulePath = "example.com/fixtureapp"

// renderSkeleton writes the embedded skeleton into dir using the given layout,
// mirroring runNew's walk: dotfile renames, .tmpl rendering, and the
// GraphQL-only skip list.
func renderSkeleton(t *testing.T, dir, layout string, graphQL bool) {
	t.Helper()

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
func inRenderedProject(t *testing.T, layout string) string {
	t.Helper()
	dir := t.TempDir()
	renderSkeleton(t, dir, layout, false)

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
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
