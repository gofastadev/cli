package commands

import (
	"bytes"
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
