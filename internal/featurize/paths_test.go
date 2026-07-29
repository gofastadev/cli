package featurize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerResourceMapping_AllPaths(t *testing.T) {
	pairs := PerResourceMapping("user")
	wantCount := 15 // models + validators stay in shared layered dirs; dtos collapse into feature per Option B
	if len(pairs) != wantCount {
		t.Errorf("PerResourceMapping returned %d pairs, want %d", len(pairs), wantCount)
	}
	expectedFeaturePaths := []string{
		"app/user/service.go",
		"app/user/service_iface.go",
		"app/user/controller.go",
		"app/user/routes.go",
		"app/user/wire.go",
		"app/user/errors.go",
		"app/user/repository_iface.go",
	}
	got := make(map[string]bool)
	for _, p := range pairs {
		got[p.Feature] = true
	}
	for _, want := range expectedFeaturePaths {
		if !got[want] {
			t.Errorf("PerResourceMapping missing feature path: %s", want)
		}
	}
}

func TestSharedRelocations(t *testing.T) {
	got := SharedRelocations()
	require.Len(t, got, 1)
	assert.Equal(t, PathPair{
		Layered: "app/dtos/aliases.go",
		Feature: "app/shared/dtos/aliases.go",
	}, got[0])
}

// TestSharedRelocationsReverse pins the inversion. Note the field naming is
// deliberately reused rather than renamed: during reverse, Layered holds the
// SOURCE (feature location) and Feature holds the DESTINATION (layered
// location). Reading them the other way round would move the file backwards.
func TestSharedRelocationsReverse(t *testing.T) {
	got := SharedRelocationsReverse()
	require.Len(t, got, 1)
	assert.Equal(t, "app/shared/dtos/aliases.go", got[0].Layered, "reverse source is the feature location")
	assert.Equal(t, "app/dtos/aliases.go", got[0].Feature, "reverse destination is the layered location")
}

// TestPerResourceMapping_IsInvertedByReverseMapping is the property that
// matters most: a project taken forward to feature and back to layered must
// land on exactly the paths it started from. Any pair present in one table but
// missing (or spelled differently) in the other would strand a file.
func TestPerResourceMapping_IsInvertedByReverseMapping(t *testing.T) {
	const snake = "user"

	forward := PerResourceMapping(snake)
	reverse := ReversePerResourceMapping(snake)
	require.Equal(t, len(forward), len(reverse),
		"the forward and reverse tables must describe the same set of files")

	// feature path -> layered path, as claimed by the reverse table.
	back := make(map[string]string, len(reverse))
	for _, r := range reverse {
		back[r.Feature] = r.Dest.Path
	}

	for _, pair := range forward {
		dest, ok := back[pair.Feature]
		require.True(t, ok, "reverse table has no entry for feature path %q", pair.Feature)
		assert.Equal(t, pair.Layered, dest,
			"round-trip mismatch: %s -> %s -> %s", pair.Layered, pair.Feature, dest)
	}
}

// TestReversePerResourceMapping_PackageNames pins the package decl each
// reverse destination must carry. Getting one wrong produces a file whose
// package clause disagrees with its directory — a compile error in the user's
// project, surfacing well after the refactor has already rewritten the tree.
func TestReversePerResourceMapping_PackageNames(t *testing.T) {
	want := map[string]string{
		"app/user/dtos.go":             "dtos",
		"app/user/dtos_test.go":        "dtos_test",
		"app/user/repository.go":       "repositories",
		"app/user/repository_iface.go": "interfaces",
		"app/user/repository_test.go":  "repositories_test",
		"app/user/service.go":          "services",
		"app/user/service_iface.go":    "interfaces",
		"app/user/service_test.go":     "services_test",
		"app/user/errors.go":           "services",
		"app/user/inputs.go":           "services",
		"app/user/inputs_test.go":      "services_test",
		"app/user/controller.go":       "controllers",
		"app/user/controller_test.go":  "controllers_test",
		"app/user/routes.go":           "routes",
		"app/user/wire.go":             "providers",
	}

	got := ReversePerResourceMapping("user")
	require.Len(t, got, len(want))
	for _, entry := range got {
		expected, ok := want[entry.Feature]
		require.True(t, ok, "unexpected reverse entry %q", entry.Feature)
		assert.Equal(t, expected, entry.Dest.PackageName, "package name for %s", entry.Feature)
	}
}

// TestPerResourceMapping_ExcludesCollapsedFiles documents an intentional
// absence: models and validators do NOT move into the feature package, so a
// caller iterating this table must not expect to relocate them.
func TestPerResourceMapping_ExcludesCollapsedFiles(t *testing.T) {
	for _, pair := range PerResourceMapping("user") {
		assert.NotContains(t, pair.Layered, "app/models/",
			"models stay in package models — see collapsablePaths")
		assert.NotContains(t, pair.Layered, "app/validators/",
			"validators stay in package validators")
	}
}

// TestPerResourceMapping_UsesSnakeThroughout guards against a mapping entry
// that hardcodes a resource name instead of interpolating the argument.
func TestPerResourceMapping_UsesSnakeThroughout(t *testing.T) {
	for _, pair := range PerResourceMapping("purchase_order") {
		assert.Contains(t, pair.Layered+pair.Feature, "purchase_order")
	}
}
