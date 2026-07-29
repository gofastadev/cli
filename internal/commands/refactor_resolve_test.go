package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/featurize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage for refactor.go's name/resource resolution and its small string
// helpers. These decide WHICH resources the refactor touches, so a wrong
// answer here either skips a resource or invents one that does not exist.

// --- string helpers ---

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

// TestPluralizeSimple covers each arm of the pluralisation switch. The plural
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

// --- readModulePath ---

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

// --- runGoCommand ---

// TestRunGoCommand covers the thin exec wrapper in both directions. `go env`
// is used because it is fast, side-effect free, and always available.
func TestRunGoCommand(t *testing.T) {
	assert.NoError(t, runGoCommand("env", "GOPATH"))
	assert.Error(t, runGoCommand("definitely-not-a-go-subcommand"),
		"a failing go invocation must surface as an error")
}

// --- resolveRefactorResources (forward) ---

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

// --- resolveLayeredRevertResources (reverse) ---

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

// --- eligibility guards ---

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

// --- refactorResolvesUser ---

func TestRefactorResolvesUser(t *testing.T) {
	assert.True(t, refactorResolvesUser([]featurize.Resource{
		{Snake: "order"}, {Snake: "user"},
	}))
	assert.False(t, refactorResolvesUser([]featurize.Resource{{Snake: "order"}}))
	assert.False(t, refactorResolvesUser(nil))
}
