package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteTemplate_MkdirAllError(t *testing.T) {
	setupTempProject(t)
	// Make "output" a regular file so MkdirAll("output") inside
	// WriteTemplate fails with ENOTDIR.
	makeParentAFile(t, "output")
	err := WriteTemplate("output/foo.go", "x", "package foo", sampleScaffoldData())
	assert.Error(t, err)
}

func TestWriteTemplate_CreateError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based write denial")
	}
	setupTempProject(t)
	// Create "output" as a read+execute-only directory. MkdirAll("output")
	// sees it already exists and returns nil, then os.Create("output/foo.go")
	// fails with EACCES.
	require.NoError(t, os.Mkdir("output", 0o555))
	t.Cleanup(func() { _ = os.Chmod("output", 0o755) })
	err := WriteTemplate("output/foo.go", "x", "package foo", sampleScaffoldData())
	assert.Error(t, err)
}

func TestWriteTemplate_DryRunRecordsButDoesNotWrite(t *testing.T) {
	resetPlannerState(t)
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	d := ScaffoldData{Name: "Product", SnakeName: "product", ModulePath: "example.com/app"}
	err := WriteTemplate("app/models/product.model.go", "model",
		"package models\n\ntype {{.Name}} struct{}\n", d)
	require.NoError(t, err)

	// Disk must be untouched.
	_, statErr := os.Stat("app/models/product.model.go")
	assert.True(t, os.IsNotExist(statErr), "dry-run must not create files on disk")

	// Plan must record exactly one create action.
	plan := Plan()
	require.Len(t, plan, 1)
	assert.Equal(t, "create", plan[0].Kind)
	assert.Equal(t, "app/models/product.model.go", plan[0].Path)
	assert.Greater(t, plan[0].Size, 0)
}

func TestWriteTemplate_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/test.go"
	tmpl := "package {{.SnakeName}}\n\ntype {{.Name}} struct{}\n"

	err := WriteTemplate(path, "test", tmpl, d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Contains(t, content, "package product")
	assert.Contains(t, content, "type Product struct{}")
}

func TestWriteTemplate_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/test.go"

	writeTestFile(t, path, "original content")

	err := WriteTemplate(path, "test", "new content {{.Name}}", d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Equal(t, "original content", content)
}

func TestWriteTemplate_CreatesParentDirs(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "a/b/c/deep.go"
	tmpl := "package deep"

	err := WriteTemplate(path, "test", tmpl, d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	// `.go` outputs are formatted through go/format.Source, which
	// guarantees the trailing-newline convention `gofmt -s -l` enforces.
	assert.Equal(t, "package deep\n", content)
}

func TestWriteTemplate_InvalidTemplate(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/bad.go"

	err := WriteTemplate(path, "bad", "{{.InvalidSyntax", d)
	assert.Error(t, err)

	// File should not exist
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestWriteTemplate_FuncMap(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/funcmap.go"
	tmpl := "braces: {{lbrace}} {{rbrace}}"

	err := WriteTemplate(path, "test", tmpl, d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Contains(t, content, "braces: { }")
}

func TestWriteTemplate_TemplateExecutionError(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/exec_error.go"
	// Template references a method that doesn't exist on ScaffoldData
	tmpl := "{{.NonExistentMethod}}"

	err := WriteTemplate(path, "test", tmpl, d)
	assert.Error(t, err)
}

// TestWriteTemplate_RejectsAbsolutePath — the planner's
// ensureWithinProject guard refuses to write an output file to an
// absolute path. Defense-in-depth against a name-derived path that
// escapes the project tree; every production generator writes to a
// relative path under the project root.
func TestWriteTemplate_RejectsAbsolutePath(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := filepath.Join(t.TempDir(), "sub", "file.go")

	err := WriteTemplate(path, "test", "package sub", d)
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
}

// TestWriteTemplate_RejectsTraversalPath — a relative output path that
// climbs out of the project root with `..` is rejected by the same
// guard.
func TestWriteTemplate_RejectsTraversalPath(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := WriteTemplate("../escape/file.go", "test", "package escape", d)
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
}

// TestWriteTemplate_UsesTimestamp — a template that calls
// {{timestamp}} resolves and writes the file. Previously uncovered
// the `timestamp` function inside the FuncMap because no shipped
// template references it.
func TestWriteTemplate_UsesTimestamp(t *testing.T) {
	setupTempProject(t)
	path := "out.txt"
	data := sampleScaffoldData()
	err := WriteTemplate(path, "t", `{{timestamp}} {{lbrace}} x {{rbrace}}`, data)
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	// RFC3339 timestamp starts with 4-digit year. Just check the closing brace.
	assert.Contains(t, string(body), "{ x }")
}

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

// makeParentAFile replaces the given path with a regular file so that any
// subsequent MkdirAll on it returns an error. Parent directories are
// created first so only the leaf component is a file.
func makeParentAFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("not a dir"), 0o644))
}

// mkReadOnlyLeaf creates parent dirs at 0o755 then the leaf at 0o555 so the
// leaf exists (MkdirAll is a no-op in the generator) but writes inside fail.
func mkReadOnlyLeaf(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Mkdir(path, 0o555))
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

// writeTestFile is a helper to write a file in the current temp directory.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// readTestFile reads a file and returns its content.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
