package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression coverage for the Go-floor breakage.
//
// A tool dependency lives in the generated project's go.mod, so its own `go`
// directive raises the project's. air v1.67.2 moved to `go 1.26.0`, which
// silently bumped every new scaffold from 1.25.0 to 1.26.0 and broke that
// project's `make lint` — golangci-lint is itself a go1.25 module and refuses
// to lint a module targeting a newer language version. The failure surfaced
// several steps away from its cause, which is what these tests exist to
// prevent recurring.

// TestToolVersions_ArePinned guards the pinning decision itself: an @latest
// tool is exactly how the floor moved without anyone choosing it.
func TestToolVersions_ArePinned(t *testing.T) {
	versions := map[string]string{
		"gqlgen":       toolVersionGqlgen,
		"wire":         toolVersionWire,
		"air":          toolVersionAir,
		"swag":         toolVersionSwag,
		"http-swagger": toolVersionHTTPSwagger,
		"chi":          toolVersionChi,
	}
	for name, v := range versions {
		t.Run(name, func(t *testing.T) {
			assert.NotEqual(t, "latest", v, "tool versions must be pinned, not tracked")
			assert.True(t, strings.HasPrefix(v, "v"), "want a semver tag, got %q", v)
		})
	}
}

// TestToolVersionAir_StaysBelowTheGoFloorBump pins the specific version that
// caused the incident. air v1.67.2 is the first release declaring go 1.26.0;
// moving to it (or later) without also raising scaffoldGoVersion and the
// linter reintroduces the exact failure.
func TestToolVersionAir_StaysBelowTheGoFloorBump(t *testing.T) {
	assert.Equal(t, "v1.67.1", toolVersionAir,
		"air v1.67.2+ declares go 1.26.0 and raises the scaffold's Go floor; "+
			"bumping this requires raising scaffoldGoVersion and the pinned golangci-lint together")
}

// TestScaffoldGoVersion_MatchesSkeletonAndRepo keeps the three places that
// declare the support floor from drifting apart.
func TestScaffoldGoVersion_MatchesSkeletonAndRepo(t *testing.T) {
	root := repoRoot(t)

	goVersionFile, err := os.ReadFile(filepath.Join(root, "internal", "skeleton", "project", "dot-go-version"))
	require.NoError(t, err)
	assert.Equal(t, scaffoldGoVersion, strings.TrimSpace(string(goVersionFile)),
		"skeleton dot-go-version must match scaffoldGoVersion")

	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, scaffoldGoVersion, readGoDirectiveFromBytes(goMod),
		"this repo's go directive must match the floor it generates")
}

// --- readGoDirective ---

func TestReadGoDirective(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]struct {
		content string
		want    string
	}{
		"plain": {"module example.com/a\n\ngo 1.25.0\n", "1.25.0"},
		"with toolchain": {
			"module example.com/a\n\ngo 1.26.0\n\ntoolchain go1.26.1\n", "1.26.0",
		},
		"with requires": {
			"module example.com/a\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/x/y v1.0.0\n)\n", "1.25.0",
		},
		"no directive": {"module example.com/a\n", ""},
		// A `go` inside a require block must not be mistaken for the directive.
		"go-prefixed require": {
			"module example.com/a\n\nrequire golang.org/x/tools v0.45.0\n", "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".mod")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o644))
			assert.Equal(t, tc.want, readGoDirective(path))
		})
	}
}

func TestReadGoDirective_UnreadableFile(t *testing.T) {
	assert.Empty(t, readGoDirective(filepath.Join(t.TempDir(), "does-not-exist.mod")),
		"a missing go.mod yields no directive rather than a panic")
}

// --- verifyGoFloor ---

// TestVerifyGoFloor_WarnsOnlyWhenTheFloorMoved drives the guard directly. It
// warns rather than aborting: by this point the project is written and
// otherwise usable, and the developer can pin the offending tool themselves.
func TestVerifyGoFloor_WarnsOnlyWhenTheFloorMoved(t *testing.T) {
	cases := map[string]struct {
		goMod    string
		wantWarn bool
	}{
		"at the floor":     {"module example.com/a\n\ngo " + scaffoldGoVersion + "\n", false},
		"raised by a tool": {"module example.com/a\n\ngo 1.26.0\n", true},
		"no go.mod at all": {"", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			orig, err := os.Getwd()
			require.NoError(t, err)
			t.Cleanup(func() { _ = os.Chdir(orig) })
			require.NoError(t, os.Chdir(dir))

			if tc.goMod != "" {
				require.NoError(t, os.WriteFile("go.mod", []byte(tc.goMod), 0o644))
			}

			out := captureStdout(t, func() { verifyGoFloor("myapp") })

			if tc.wantWarn {
				assert.Contains(t, out, "raised this project's Go version to 1.26.0")
				assert.Contains(t, out, "make lint", "the warning must name the concrete consequence")
			} else {
				assert.NotContains(t, out, "raised this project's Go version")
			}
		})
	}
}

// readGoDirectiveFromBytes is the in-memory twin of readGoDirective, used to
// check this repo's own go.mod without writing a temp copy.
func readGoDirectiveFromBytes(b []byte) string {
	m := goDirectivePattern.FindSubmatch(b)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "walked past the filesystem root without finding go.mod")
		dir = parent
	}
}
