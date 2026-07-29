package generate

import (
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- shouldFeaturizeGenOutput ---

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

// --- WriteTemplate, feature transform ---

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

// --- writeOrRecordPatch containment check ---

// TestWriteOrRecordPatch_RefusesOutOfTreePath covers the defense-in-depth net
// that stops a resource name which slipped past validateIdentifier from
// patching a file outside the project root.
func TestWriteOrRecordPatch_RefusesOutOfTreePath(t *testing.T) {
	setupTempProject(t)

	for _, path := range []string{"../escape.go", "/etc/passwd", ".."} {
		t.Run(path, func(t *testing.T) {
			err := writeOrRecordPatch(path, "attempted patch", []byte("package x\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to write outside the project root")
		})
	}
}

// --- qualifyLocalTypes and friends ---

// mustParseType parses a type expression for the qualifier tests.
func mustParseType(t *testing.T, src string) ast.Expr {
	t.Helper()
	e, err := parser.ParseExpr(src)
	require.NoError(t, err)
	return e
}

// typeString renders an expression back to source for comparison.
func typeString(t *testing.T, e ast.Expr) string {
	t.Helper()
	return exprString(e)
}

func TestQualifyLocalTypes_NilAndEmptyPackage(t *testing.T) {
	assert.Nil(t, qualifyLocalTypes(nil, "interfaces"))

	// An empty package name means "nothing to qualify with" — the expression
	// must come back untouched rather than gaining a leading dot.
	e := mustParseType(t, "Filters")
	assert.Equal(t, e, qualifyLocalTypes(e, ""))
}

// TestQualifyLocalTypes_Generics covers the two generic-instantiation nodes.
// A mock for an interface with generic parameters would otherwise emit
// unqualified type arguments and fail to compile from package mocks.
func TestQualifyLocalTypes_Generics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"single type arg", "Result[Filters]", "interfaces.Result[interfaces.Filters]"},
		{"single builtin arg", "Result[string]", "interfaces.Result[string]"},
		{"several type args", "Pair[Filters, Attachment]", "interfaces.Pair[interfaces.Filters, interfaces.Attachment]"},
		{"mixed args", "Pair[string, Filters]", "interfaces.Pair[string, interfaces.Filters]"},
		{"parenthesized", "(Filters)", "(interfaces.Filters)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := qualifyLocalTypes(mustParseType(t, tc.src), "interfaces")
			assert.Equal(t, tc.want, typeString(t, got))
		})
	}
}

func TestQualifyFieldList_Nil(t *testing.T) {
	assert.Nil(t, qualifyFieldList(nil, "interfaces"),
		"a func type with no results has a nil field list")
}

// TestQualifyLocalTypes_FuncTypeWithoutResults exercises the func-type arm
// through a signature whose Results list is nil.
func TestQualifyLocalTypes_FuncTypeWithoutResults(t *testing.T) {
	got := qualifyLocalTypes(mustParseType(t, "func(Filters)"), "interfaces")
	assert.Equal(t, "func(interfaces.Filters)", typeString(t, got))
}

// TestQualifyLocalTypes_ExoticTypesPassThrough covers the fall-through arm.
// Struct and interface literals in a signature are left verbatim: their field
// types would need qualifying too, and an interface method set written inline
// is rare enough that rewriting it is not worth the risk of corrupting it.
func TestQualifyLocalTypes_ExoticTypesPassThrough(t *testing.T) {
	for _, src := range []string{
		"struct{ X Filters }",
		"interface{ Do() error }",
		"chan<- struct{}",
	} {
		t.Run(src, func(t *testing.T) {
			e := mustParseType(t, src)
			got := qualifyLocalTypes(e, "interfaces")
			assert.Equal(t, typeString(t, e), typeString(t, got),
				"composite literal types must pass through unchanged")
		})
	}
}
