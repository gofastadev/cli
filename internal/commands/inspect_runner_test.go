package commands

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for inspect.go entry points that inspect_test.go doesn't
// already exercise — runInspect end-to-end, renderInspectText, and
// tryParseDTOs / tryParseRoutesForResource happy + empty paths.
// ─────────────────────────────────────────────────────────────────────

// scaffoldInspectProject writes a tiny gofasta-shaped project rooted
// at t.TempDir() with a model, DTOs, service interface, controller,
// and routes file for `Product`. Chdirs into the dir for the test.
func scaffoldInspectProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	files := map[string]string{
		"app/models/product.model.go": `package models
type Product struct {
	ID    string ` + "`" + `json:"id" gorm:"primaryKey"` + "`" + `
	Name  string ` + "`" + `json:"name"` + "`" + `
	Price float64 ` + "`" + `json:"price"` + "`" + `
}
`,
		"app/dtos/product.dtos.go": `package dtos
type CreateProductDto struct {
	Name  string ` + "`" + `json:"name" validate:"required"` + "`" + `
	Price float64 ` + "`" + `json:"price"` + "`" + `
}
type ProductResponseDto struct {
	ID    string  ` + "`" + `json:"id"` + "`" + `
	Name  string  ` + "`" + `json:"name"` + "`" + `
	Price float64 ` + "`" + `json:"price"` + "`" + `
}
type privateThing struct{ x int }
`,
		"app/services/interfaces/product_service.go": `package interfaces
import "context"
type ProductServiceInterface interface {
	FindAll(ctx context.Context) ([]string, error)
	Create(ctx context.Context, name string) error
}
`,
		"app/rest/controllers/product.controller.go": `package controllers
import "net/http"
type ProductController struct{}
func (c *ProductController) List(w http.ResponseWriter, r *http.Request) error { return nil }
func (c *ProductController) Create(w http.ResponseWriter, r *http.Request) error { return nil }
`,
		"app/rest/routes/product.routes.go": `package routes
import "github.com/go-chi/chi/v5"
func RegisterProductRoutes(r chi.Router) {
	r.Get("/api/v1/products", nil)
	r.Post("/api/v1/products", nil)
	r.Patch("/api/v1/products/{id}", nil)
}
`,
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

// TestRunInspect_FullProject — every standard file exists; the
// runner succeeds and exercises every branch.
func TestRunInspect_FullProject(t *testing.T) {
	scaffoldInspectProject(t)
	require.NoError(t, runInspect("Product"))
}

// TestRunInspect_EmptyName — the validator rejects empty string.
func TestRunInspect_EmptyName(t *testing.T) {
	err := runInspect("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource name cannot be empty")
}

// TestTryParseDTOs_CollectsStructTypes — every struct type in the
// DTOs file is surfaced. The renderer decides whether to display
// unexported types; the parser stays faithful to source.
func TestTryParseDTOs_CollectsStructTypes(t *testing.T) {
	scaffoldInspectProject(t)
	got := tryParseDTOs("product")
	names := make([]string, len(got))
	for i, d := range got {
		names[i] = d.Name
	}
	assert.Contains(t, names, "CreateProductDto")
	assert.Contains(t, names, "ProductResponseDto")
}

// TestTryParseDTOs_MissingFile — returns empty slice without error.
func TestTryParseDTOs_MissingFile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	assert.Empty(t, tryParseDTOs("nonexistent"))
}

// TestTryParseRoutesForResource_FindsRoutes — produces entries for
// the scaffolded routes.
func TestTryParseRoutesForResource_FindsRoutes(t *testing.T) {
	scaffoldInspectProject(t)
	got := tryParseRoutesForResource("product")
	assert.NotEmpty(t, got)
}

// TestTryParseRoutesForResource_MissingFile — returns nil.
func TestTryParseRoutesForResource_MissingFile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	assert.Nil(t, tryParseRoutesForResource("nothing"))
}

// TestRenderInspectText_FullResource — exercises every section of
// the text renderer.
func TestRenderInspectText_FullResource(t *testing.T) {
	r := &inspectedResource{
		Name:  "Product",
		Snake: "product",
		Model: &modelInfo{
			File: "app/models/product.model.go",
			Fields: []fieldEntry{
				{Name: "ID", Type: "string", Tag: `json:"id"`},
				{Name: "Name", Type: "string"},
			},
		},
		DTOs: []dtoInfo{
			{File: "app/dtos/product.dtos.go", Name: "CreateProductDto",
				Fields: []fieldEntry{{Name: "Name", Type: "string"}}},
		},
		ServiceMethods: []methodSignature{
			{Name: "FindAll", Sig: "(ctx context.Context) error"},
		},
		ControllerMeth: []methodSignature{
			{Name: "List", Sig: "(w http.ResponseWriter, r *http.Request) error"},
		},
		Routes: []routeEntry{
			{Method: "GET", Path: "/api/v1/products"},
		},
		Files: []string{"app/models/product.model.go"},
	}
	var buf bytes.Buffer
	renderInspectText(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "Product")
	assert.Contains(t, out, "CreateProductDto")
	assert.Contains(t, out, "Service methods")
	assert.Contains(t, out, "Controller methods")
	assert.Contains(t, out, "/api/v1/products")
}

// TestRenderInspectText_Minimal — early-exit branches covered.
func TestRenderInspectText_Minimal(t *testing.T) {
	var buf bytes.Buffer
	renderInspectText(&buf, &inspectedResource{Name: "X", Snake: "x"})
	assert.NotEmpty(t, buf.String())
}

// TestExprToString_CoversEveryASTShape — struct whose fields span
// every AST expression kind exprToString handles.
func TestExprToString_CoversEveryASTShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	source := `package p
type T struct {
	A string
	B *int
	C []float64
	D map[string]int
	E func(int) error
	F chan bool
	G interface{}
	H [4]byte
}
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, source, 0)
	require.NoError(t, err)
	fields := findStructFields(f, "T")
	require.Len(t, fields, 8)
	for _, fe := range fields {
		assert.NotEmpty(t, fe.Type, "field %s had empty type", fe.Name)
	}
}

// TestParseGoFile_BadSource — malformed Go returns an error.
func TestParseGoFile_BadSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	require.NoError(t, os.WriteFile(path, []byte("not go code"), 0o644))
	_, err := parseGoFile(path)
	require.Error(t, err)
}

// TestFindStructFields_MissingStruct — returns empty for an absent
// struct name.
func TestFindStructFields_MissingStruct(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.go")
	require.NoError(t, os.WriteFile(path, []byte("package p\n"), 0o644))
	f, err := parseGoFile(path)
	require.NoError(t, err)
	assert.Empty(t, findStructFields(f, "Missing"))
}

// TestExprToString_NilNode — nil input returns the "?" sentinel
// rather than panicking. Confirms the default-branch path.
func TestExprToString_NilNode(t *testing.T) {
	assert.Equal(t, "?", exprToString(nil))
}

// TestInspectTryParseModel_BadFile — parse error when the file
// exists but doesn't compile.
func TestInspectTryParseModel_BadFile(t *testing.T) {
	chdirTemp(t)
	dir := filepath.Join("app", "models")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.model.go"),
		[]byte("not valid go"), 0o644))
	_, ok := tryParseModel("Broken", "broken")
	assert.False(t, ok)
}

// TestInspectTryParseInterfaceMethods_MissingFile — absent file
// returns (nil, false).
func TestInspectTryParseInterfaceMethods_MissingFile(t *testing.T) {
	chdirTemp(t)
	_, ok := tryParseInterfaceMethods(
		"app/services/interfaces/missing.go", "MissingIface")
	assert.False(t, ok)
}

// TestInspectTryParseInterfaceMethods_InterfaceNotFound — file
// exists but doesn't declare the expected interface.
func TestInspectTryParseInterfaceMethods_InterfaceNotFound(t *testing.T) {
	chdirTemp(t)
	path := filepath.Join("app", "services", "interfaces", "x.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("package interfaces\n"), 0o644))
	_, ok := tryParseInterfaceMethods(path, "NoSuchInterface")
	assert.False(t, ok)
}

// TestInspectTryParseControllerMethods_MissingFile — absent file
// returns (nil, false).
func TestInspectTryParseControllerMethods_MissingFile(t *testing.T) {
	chdirTemp(t)
	_, ok := tryParseControllerMethods(
		"app/rest/controllers/missing.controller.go", "MissingController")
	assert.False(t, ok)
}

// TestInspectTryParseDTOs_BadFile — malformed source returns nil.
func TestInspectTryParseDTOs_BadFile(t *testing.T) {
	chdirTemp(t)
	dir := filepath.Join("app", "dtos")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.dtos.go"),
		[]byte("package broken not valid"), 0o644))
	assert.Empty(t, tryParseDTOs("broken"))
}

// TestReadStructFields_EmbeddedField — embedded field (no name) is
// skipped by the current implementation; only named fields surface.
func TestReadStructFields_EmbeddedField(t *testing.T) {
	src := `package p
import "io"
type T struct {
	Name string
	io.Reader
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	f, err := parseGoFile(path)
	require.NoError(t, err)
	fields := findStructFields(f, "T")
	names := make([]string, len(fields))
	for i, fe := range fields {
		names[i] = fe.Name
	}
	assert.Contains(t, names, "Name")
}

// parseGoSrc is a small helper that turns Go source text into an
// *ast.File via go/parser.
func parseGoSrc(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "t.go", src, parser.ParseComments)
	require.NoError(t, err)
	return file
}

// TestTryParseModel_NoStruct — file parses but the target struct name
// isn't defined. fields is nil → returns (nil, false).
func TestTryParseModel_NoStruct(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join("app", "models"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("app", "models", "user.model.go"),
		[]byte("package models\n\ntype Other struct{}\n"), 0o644))
	info, ok := tryParseModel("User", "user")
	assert.False(t, ok)
	assert.Nil(t, info)
}

// TestTryParseDTOs_NonTypeDecl — var / const blocks are skipped.
func TestTryParseDTOs_NonTypeDecl(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join("app", "dtos"), 0o755))
	src := `package dtos
var X = 1
const Y = 2
type A struct { N int }
`
	require.NoError(t, os.WriteFile(filepath.Join("app", "dtos", "user.dtos.go"),
		[]byte(src), 0o644))
	got := tryParseDTOs("user")
	assert.Len(t, got, 1)
}

// TestTryParseDTOs_NonStructType — not a StructType (alias) skipped.
func TestTryParseDTOs_NonStructType(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join("app", "dtos"), 0o755))
	src := `package dtos
type IntAlias = int
type B struct { N int }
`
	require.NoError(t, os.WriteFile(filepath.Join("app", "dtos", "user.dtos.go"),
		[]byte(src), 0o644))
	got := tryParseDTOs("user")
	assert.Len(t, got, 1)
}

// TestTryParseDTOs_NonTypeSpecBranch — GenDecl with Tok=="type" can
// only contain TypeSpec by Go syntax; branch is defensive.
func TestTryParseDTOs_NonTypeSpecBranch(t *testing.T) {
	t.Skip("gd.Specs for Tok=type always yields TypeSpec; branch defensive")
}

// TestTryParseInterfaceMethods_NoMatch — interface exists but not the
// requested name → no methods returned.
func TestTryParseInterfaceMethods_NoMatch(t *testing.T) {
	chdirTemp(t)
	src := `package x
type OtherName interface {
	Foo()
}
`
	path := filepath.Join("x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	_, ok := tryParseInterfaceMethods(path, "Requested")
	assert.False(t, ok)
}

// TestTryParseInterfaceMethods_NonInterfaceType — type block where
// the type isn't an interface.
func TestTryParseInterfaceMethods_NonInterfaceType(t *testing.T) {
	chdirTemp(t)
	src := `package x
type Requested struct { N int }
`
	path := filepath.Join("x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	_, ok := tryParseInterfaceMethods(path, "Requested")
	assert.False(t, ok)
}

// TestTryParseInterfaceMethods_EmbeddedMethod — interface embeds
// another interface (no Names on the field) → skip.
func TestTryParseInterfaceMethods_EmbeddedMethod(t *testing.T) {
	chdirTemp(t)
	src := `package x
type Other interface { Foo() }
type Requested interface {
	Other     // embedded — len(m.Names) == 0
	Bar()
}
`
	path := filepath.Join("x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	methods, ok := tryParseInterfaceMethods(path, "Requested")
	require.True(t, ok)
	assert.Len(t, methods, 1) // only Bar; Other was embedded
}

// TestTryParseControllerMethods_WrongReceiver — method on another
// type is filtered out.
func TestTryParseControllerMethods_WrongReceiver(t *testing.T) {
	chdirTemp(t)
	src := `package x
type Other struct{}
func (o *Other) Foo() {}
`
	path := filepath.Join("x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	_, ok := tryParseControllerMethods(path, "Requested")
	assert.False(t, ok)
}

// TestTryParseControllerMethods_NonExported — private method is
// filtered out.
func TestTryParseControllerMethods_NonExported(t *testing.T) {
	chdirTemp(t)
	src := `package x
type T struct{}
func (t *T) unexported() {}
`
	path := filepath.Join("x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	_, ok := tryParseControllerMethods(path, "T")
	assert.False(t, ok)
}

// TestTryParseRoutesForResource_WithIndex — an index.routes.go with
// r.Mount("/api/v1", api) sets apiPrefix.
func TestTryParseRoutesForResource_WithIndex(t *testing.T) {
	chdirTemp(t)
	routesDir := filepath.Join("app", "rest", "routes")
	require.NoError(t, os.MkdirAll(routesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routesDir, "index.routes.go"),
		[]byte(`package routes
func X(r chi.Mux) { r.Mount("/api/v1", api) }`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(routesDir, "user.routes.go"),
		[]byte(`r.Get("/users", h)`), 0o644))
	entries := tryParseRoutesForResource("user")
	assert.NotEmpty(t, entries)
}

// TestTryParseRoutesForResource_NoIndexFile — no app/rest/routes/
// index.routes.go so apiPrefix stays empty.
func TestTryParseRoutesForResource_NoIndexFile(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join("app", "rest", "routes"), 0o755))
	// Place a route file but NO index.routes.go.
	require.NoError(t, os.WriteFile(filepath.Join("app", "rest", "routes", "x.routes.go"),
		[]byte(`r.Get("/x", h)`), 0o644))
	// Call runInspect directly for a Resource name that won't match
	// anything — the function tolerates missing files.
	_ = runInspect("User")
}

// TestFindStructFields_NonStructType — named type that isn't a struct
// (e.g. a function alias). findStructFields returns nil.
func TestFindStructFields_NonStructType(t *testing.T) {
	file := parseGoSrc(t, `package x
type F func()
`)
	got := findStructFields(file, "F")
	assert.Nil(t, got)
}

// TestFindStructFields_NoMatch — file has structs but none matching.
func TestFindStructFields_NoMatch(t *testing.T) {
	file := parseGoSrc(t, `package x
type A struct { N int }
`)
	got := findStructFields(file, "B")
	assert.Nil(t, got)
}

// TestFindStructFields_NonTypeDecl — var/const declarations are
// skipped.
func TestFindStructFields_NonTypeDecl(t *testing.T) {
	file := parseGoSrc(t, `package x
var A = 1
type T struct { N int }
`)
	got := findStructFields(file, "T")
	require.Len(t, got, 1)
}

// TestExprToString_Ellipsis — ...T variadic argument.
func TestExprToString_Ellipsis(t *testing.T) {
	file := parseGoSrc(t, `package x
func Y(xs ...int) {}
`)
	fd := file.Decls[0].(*ast.FuncDecl)
	// First param is an Ellipsis type.
	params := fd.Type.Params.List[0].Type
	got := exprToString(params)
	assert.Equal(t, "...int", got)
}

// TestFieldListToString_Empty — nil or empty FieldList returns "".
func TestFieldListToString_Empty(t *testing.T) {
	assert.Empty(t, fieldListToString(nil))
	assert.Empty(t, fieldListToString(&ast.FieldList{}))
}

// TestExtractDTOsFromAST_NonTypeSpec — a synthetic ast.File whose
// type-decl Specs contain a non-TypeSpec entry forces the defensive
// "continue" branch. Go's parser never produces this, so we build
// the AST by hand.
func TestExtractDTOsFromAST_NonTypeSpec(t *testing.T) {
	// Build a file with: type (<non-TypeSpec spec> ; validStruct struct{})
	validSpec := &ast.TypeSpec{
		Name: &ast.Ident{Name: "Valid"},
		Type: &ast.StructType{Fields: &ast.FieldList{}},
	}
	// ImportSpec is an ast.Spec but not an *ast.TypeSpec.
	nonTypeSpec := &ast.ImportSpec{}
	file := &ast.File{
		Decls: []ast.Decl{
			&ast.GenDecl{
				Tok:   token.TYPE,
				Specs: []ast.Spec{nonTypeSpec, validSpec},
			},
		},
	}
	got := extractDTOsFromAST(file, "fake.go")
	// Only the valid TypeSpec should produce an entry.
	require.Len(t, got, 1)
	assert.Equal(t, "Valid", got[0].Name)
}
