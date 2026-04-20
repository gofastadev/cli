package commands

import (
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
// Coverage for inspect.go AST parsers. Creates small Go files on disk
// that the parser walks end-to-end.
// ─────────────────────────────────────────────────────────────────────

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

// TestTryParseDTOs_NonTypeSpec — not a TypeSpec (alias) skipped.
func TestTryParseDTOs_NonStructType(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join("app", "dtos"), 0o755))
	// Type alias (ImportSpec isn't used here but `type X = int` uses TypeSpec).
	// Non-struct type ts.Type is an Ident, not *ast.StructType → continue.
	src := `package dtos
type IntAlias = int
type B struct { N int }
`
	require.NoError(t, os.WriteFile(filepath.Join("app", "dtos", "user.dtos.go"),
		[]byte(src), 0o644))
	got := tryParseDTOs("user")
	assert.Len(t, got, 1)
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
