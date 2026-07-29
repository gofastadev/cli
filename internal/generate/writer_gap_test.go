// Coverage for writer.go — the feature-layout transform applied to
// generated output, and the path rule that decides when it applies.

package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShouldFeaturizeGenOutput draws the line the feature transform respects:
// only Go files inside the resource's own package are rewritten. A migration,
// a GraphQL schema, or a file emitted to a shared directory keeps its package
// decl and symbol names, so running featurize over them would corrupt output
// that is identical in both layouts.
func TestShouldFeaturizeGenOutput(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		snake string
		want  bool
	}{
		{"go file in the feature dir", "app/product/service.go", "product", true},
		{"nested go file in the feature dir", "app/product/sub/thing.go", "product", true},
		{"shared models dir", "app/models/product.model.go", "product", false},
		{"shared validators dir", "app/validators/product.validators.go", "product", false},
		{"another feature's dir", "app/order/service.go", "product", false},
		{"sql migration", "db/migrations/000001_create.up.sql", "product", false},
		{"graphql schema", "app/product/schema.graphqls", "product", false},
		{"non-go file inside the feature dir", "app/product/README.md", "product", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shouldFeaturizeGenOutput(tc.path, tc.snake))
		})
	}
}

// TestWriteTemplate_FeatureTransformsOwnPackageFiles covers the featurize arm.
// The template renders with the layered package name; the transform is what
// rewrites it to match the destination directory.
func TestWriteTemplate_FeatureTransformsOwnPackageFiles(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()

	tmpl := `package services

import "context"

type ProductService struct{}

func (s *ProductService) Get(ctx context.Context) error { return nil }
`
	path := filepath.Join("app", "product", "service.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, WriteTemplate(path, "svc", tmpl, d))

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "package product",
		"a file written into app/product/ must declare package product")
	assert.NotContains(t, string(body), "package services")
}

// TestWriteTemplate_FeatureLeavesSharedFilesAlone is the counterpart: the same
// project, but a destination outside the feature package.
func TestWriteTemplate_FeatureLeavesSharedFilesAlone(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()

	tmpl := "package models\n\ntype Product struct{}\n"
	path := filepath.Join("app", "models", "product.model.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, WriteTemplate(path, "model", tmpl, d))

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "package models")
}

// TestWriteTemplate_FeatureFallsBackWhenTransformFails covers the deliberate
// swallow of a transform error: featurize cannot parse non-Go-shaped output,
// and the generator still writes the untransformed bytes so the user sees a
// file (and a precise compiler error) rather than nothing at all.
func TestWriteTemplate_FeatureFallsBackWhenTransformFails(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()

	// Valid template text, but not parseable as Go: featurize must fail and
	// the rendered bytes survive unchanged.
	tmpl := "package product\n\nfunc Broken( {\n"
	path := filepath.Join("app", "product", "broken.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, WriteTemplate(path, "broken", tmpl, d))

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "func Broken(",
		"a failed transform must still emit the rendered bytes")
}
