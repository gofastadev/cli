package astpatch

import (
	"bytes"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func TestParse_ReadFileError(t *testing.T) {
	_, err := Parse("/nonexistent/path/x.go")
	require.Error(t, err)
}

func TestParse_SyntaxError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	// Half-written function — go/parser returns an error at the
	// dec.Parse call (Decorator surfaces parser errors without panic).
	require.NoError(t, os.WriteFile(path, []byte("package x\nfunc {\n"), 0o644))
	_, err := Parse(path)
	require.Error(t, err)
}

func TestRender_FormatErrorFailsLoudly(t *testing.T) {
	// A dst.File with an empty package name restores to invalid Go that
	// format.Source rejects. Render must surface that as an error —
	// returning unformatted bytes here would let a generator write
	// invalid Go back into the user's file.
	bad := &File{
		Path: "x.go",
		Dst: &dst.File{
			Name:  dst.NewIdent(""), // empty package name → invalid Go
			Decls: nil,
		},
	}
	_, err := Render(bad)
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeASTPatchFailed), ce.Code)
	require.Contains(t, ce.Error(), "gofmt of patched x.go")
}

func TestFindInterface_NotATypeDecl(t *testing.T) {
	// File contains only a func decl — no GenDecl with TYPE tok. The
	// outer continue path (line 92-93) fires.
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nfunc F() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindInterface(f, "Anything")
	require.Error(t, err)
}

func TestFindStruct_NotATypeDecl(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nfunc F() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindStruct(f, "Anything")
	require.Error(t, err)
}

func TestFindStruct_TypeIsNotStruct(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\ntype Alias = int\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindStruct(f, "Alias")
	require.Error(t, err)
}

func TestFindFunc_PackageLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nfunc Helper() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	fd, err := FindFunc(f, "", "Helper")
	require.NoError(t, err)
	require.Equal(t, "Helper", fd.Name.Name)
}

func TestFindFunc_MethodWithStarReceiver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	src := `package x
type T struct{}
func (t *T) Method() {}
`
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	fd, err := FindFunc(f, "T", "Method")
	require.NoError(t, err)
	require.Equal(t, "Method", fd.Name.Name)
}

func TestFindFunc_MethodMisnamedReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nfunc F() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindFunc(f, "Recv", "Missing")
	require.Error(t, err)
}

func TestFindFunc_PackageLevelButRecvWanted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nfunc F() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindFunc(f, "WantedRecv", "F")
	require.Error(t, err)
}

func TestFindFunc_MethodWantedButFnIsPackageLevel(t *testing.T) {
	// We ask for a method "F" on receiver "T" — there's only a
	// package-level F. The fd.Recv == nil branch fires.
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nfunc F() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindFunc(f, "T", "F")
	require.Error(t, err)
}

func TestFindFunc_WrongReceiver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\ntype T struct{}\nfunc (t T) M() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindFunc(f, "WrongRecv", "M")
	require.Error(t, err)
}

func TestInterfaceHasMethod_NilMethods(t *testing.T) {
	require.False(t, InterfaceHasMethod(&dst.InterfaceType{Methods: nil}, "X"))
}

func TestStructHasField_NilFields(t *testing.T) {
	require.False(t, StructHasField(&dst.StructType{Fields: nil}, "X"))
}

func TestAppendInterfaceMethod_NilMethodsInit(t *testing.T) {
	it := &dst.InterfaceType{Methods: nil}
	require.NoError(t, AppendInterfaceMethod(it, "Foo()"))
	require.NotNil(t, it.Methods)
}

func TestAppendInterfaceMethod_ParseFailure(t *testing.T) {
	it := &dst.InterfaceType{Methods: nil}
	require.Error(t, AppendInterfaceMethod(it, "!!!not a method!!!"))
}

func TestAppendStructField_NilFieldsInit(t *testing.T) {
	st := &dst.StructType{Fields: nil}
	require.NoError(t, AppendStructField(st, "X int"))
	require.NotNil(t, st.Fields)
}

func TestAppendStructField_ParseFailure(t *testing.T) {
	st := &dst.StructType{Fields: nil}
	require.Error(t, AppendStructField(st, "!!!not a field!!!"))
}

func TestExtractFirstSpec_NoMatchingSpec(t *testing.T) {
	// A package-level function (no TypeSpec at all) → extractFirstSpec
	// returns the zero T plus an error.
	got, err := extractFirstSpec[*dst.InterfaceType](
		"package x\nfunc F() {}\n", "func F", "x")
	require.Error(t, err)
	require.Nil(t, got)
}

func TestAppendFuncDecl_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte("package x\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)

	require.NoError(t, AppendFuncDecl(f, "func Added() {}"))
	require.Equal(t, 1, len(f.Dst.Decls))
}

func TestAppendFuncDecl_ParseFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte("package x\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	require.Error(t, AppendFuncDecl(f, "!!!not Go!!!"))
}

func TestAppendFuncDecl_NoFuncDeclInSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte("package x\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	// declSrc parses (it's a var decl), but contains no FuncDecl. The
	// final return-error branch fires.
	require.Error(t, AppendFuncDecl(f, "var Y = 1"))
}

func TestEnsureImport_AlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nimport \"fmt\"\nvar _ = fmt.Sprintf\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	require.False(t, EnsureImport(f, "fmt"))
}

func TestEnsureImport_ExtendsExistingGenDecl(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nimport \"fmt\"\nvar _ = fmt.Sprintf\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	require.True(t, EnsureImport(f, "strings"))
}

func TestEnsureImport_NoExistingImports_AddsNewBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nvar X = 1\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	require.True(t, EnsureImport(f, "fmt"))
}

func TestEnsureImport_SingleLineImportGainsParens(t *testing.T) {
	// Regression: appending to a single-line `import "context"` decl
	// without setting Lparen/Rparen rendered `import "context" "fmt"`,
	// which is invalid Go (and Render then failed).
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\n\nimport \"context\"\n\nvar _ = context.Background\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	require.True(t, EnsureImport(f, "fmt"))

	body, err := Render(f)
	require.NoError(t, err)
	_, perr := parser.ParseFile(token.NewFileSet(), "x.go", body, 0)
	require.NoError(t, perr, "patched file must remain valid Go:\n%s", body)
	require.Contains(t, string(body), "import (")
	require.Contains(t, string(body), "\"fmt\"")
}

func TestRender_RestorerError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte("package x\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)

	saved := restorerFprintFn
	restorerFprintFn = func(_ *bytes.Buffer, _ *dst.File) error { return errStubAst }
	t.Cleanup(func() { restorerFprintFn = saved })

	_, err = Render(f)
	require.Error(t, err)
}

func TestFindStruct_NameMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\ntype OtherName struct{}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindStruct(f, "WantedName")
	require.Error(t, err)
}

func TestFindFunc_PackageLevelButFnIsMethod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\ntype T struct{}\nfunc (t T) F() {}\n"), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	_, err = FindFunc(f, "", "F")
	require.Error(t, err)
}

func TestExtractFirstSpec_NonTypeSpecSkipped(t *testing.T) {
	// `import "x"` is a GenDecl whose Specs are ImportSpec, not
	// TypeSpec — extractFirstSpec's `_, ok := spec.(*dst.TypeSpec); !ok`
	// branch fires.
	src := "package x\nimport \"fmt\"\nvar _ = fmt.Sprint\n"
	got, err := extractFirstSpec[*dst.InterfaceType](src, "src", "x")
	require.Error(t, err)
	require.Nil(t, got)
}

var errStubAst = stubAstErr("stub")

func TestReceiverTypeName_Variants(t *testing.T) {
	require.Equal(t, "T", receiverTypeName(&dst.Ident{Name: "T"}))
	require.Equal(t, "T", receiverTypeName(&dst.StarExpr{X: &dst.Ident{Name: "T"}}))
	// StarExpr whose X isn't an Ident (e.g. *pkg.T which is a SelectorExpr)
	require.Equal(t, "", receiverTypeName(&dst.StarExpr{X: &dst.SelectorExpr{}}))
	// Neither Ident nor StarExpr → ""
	require.Equal(t, "", receiverTypeName(&dst.BasicLit{Kind: token.INT, Value: "1"}))
}

// writeTemp puts a temp file with the given source on disk and returns
// the path. Saves boilerplate across the table-driven cases.
func writeTemp(t *testing.T, src string) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "input.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	return path
}

func TestParse_HappyPath(t *testing.T) {
	path := writeTemp(t, "package x\n\nfunc F() {}\n")
	f, err := Parse(path)
	require.NoError(t, err)
	require.NotNil(t, f.Dst)
	require.Equal(t, "x", f.Dst.Name.Name)
}

func TestParse_SyntaxErrorReturnsClierr(t *testing.T) {
	path := writeTemp(t, "package x\n\nfunc F(\n") // unterminated
	_, err := Parse(path)
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeASTParseFailed), ce.Code)
}

func TestAppendInterfaceMethod_PreservesExistingMethodsAndComments(t *testing.T) {
	src := `package interfaces

// OrderService is the order business-logic contract.
type OrderService interface {
	// Create persists a new order.
	Create(name string) error
}
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)

	iface, err := FindInterface(f, "OrderService")
	require.NoError(t, err)
	require.False(t, InterfaceHasMethod(iface, "Archive"))

	require.NoError(t, AppendInterfaceMethod(iface, "Archive(id string) error"))

	body, err := Render(f)
	require.NoError(t, err)

	s := string(body)
	// Doc comments preserved.
	require.Contains(t, s, "// OrderService is the order business-logic contract.")
	require.Contains(t, s, "// Create persists a new order.")
	// Existing method retained.
	require.Contains(t, s, "Create(name string) error")
	// New method appended.
	require.Contains(t, s, "Archive(id string) error")
}

func TestAppendInterfaceMethod_IdempotencyCheck(t *testing.T) {
	src := `package i
type S interface {
	Already() error
}
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)
	iface, err := FindInterface(f, "S")
	require.NoError(t, err)
	require.True(t, InterfaceHasMethod(iface, "Already"))
	require.False(t, InterfaceHasMethod(iface, "Missing"))
}

func TestAppendStructField_AppendsCorrectly(t *testing.T) {
	src := `package m

type User struct {
	ID   string
	Name string ` + "`json:\"name\"`" + `
}
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)

	st, err := FindStruct(f, "User")
	require.NoError(t, err)
	require.True(t, StructHasField(st, "ID"))
	require.False(t, StructHasField(st, "DeletedAt"))

	require.NoError(t, AppendStructField(st, "DeletedAt *time.Time `gorm:\"index\"`"))

	body, err := Render(f)
	require.NoError(t, err)
	s := string(body)
	require.Contains(t, s, "DeletedAt")
	require.Contains(t, s, "gorm:\"index\"")
}

func TestFindInterface_MissingReturnsClierr(t *testing.T) {
	path := writeTemp(t, "package x\n\ntype A struct{}\n")
	f, _ := Parse(path)
	_, err := FindInterface(f, "Nope")
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeASTPatchFailed), ce.Code)
}

func TestEnsureImport_AddsAndIsIdempotent(t *testing.T) {
	src := `package x

import "fmt"

func F() { fmt.Println() }
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)

	added := EnsureImport(f, "context")
	require.True(t, added, "context should be added")

	added2 := EnsureImport(f, "context")
	require.False(t, added2, "context should not be added a second time")

	body, err := Render(f)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(body), `"context"`))
}

// parseTestFile writes src to a temp file and parses it. Shared by the
// composite-literal helper tests below.
func parseTestFile(t *testing.T, src string) *File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	f, err := Parse(path)
	require.NoError(t, err)
	return f
}

func TestFindVarCompositeLit(t *testing.T) {
	f := parseTestFile(t, `package x

var cols = []string{
	"id",
	"created_at",
}

var notALit = otherValue
`)
	lit, err := FindVarCompositeLit(f, "cols")
	require.NoError(t, err)
	require.Len(t, lit.Elts, 2)

	_, err = FindVarCompositeLit(f, "missing")
	require.Error(t, err)
	_, err = FindVarCompositeLit(f, "notALit")
	require.Error(t, err, "a var bound to a non-composite value is not a valid target")
}

func TestCompositeLitStringHelpers(t *testing.T) {
	f := parseTestFile(t, "package x\n\nvar cols = []string{\n\t\"id\",\n}\n")
	lit, err := FindVarCompositeLit(f, "cols")
	require.NoError(t, err)

	require.True(t, CompositeLitHasString(lit, "id"))
	require.False(t, CompositeLitHasString(lit, "sku"))

	AppendStringToCompositeLit(lit, "sku")
	require.True(t, CompositeLitHasString(lit, "sku"))

	out, err := Render(f)
	require.NoError(t, err)
	require.Contains(t, string(out), "\"sku\",", "appended element renders with trailing comma")
}

func TestFirstCompositeLitInFunc(t *testing.T) {
	f := parseTestFile(t, `package x

func build(m M) *Out {
	out := &Out{
		ID: m.ID,
	}
	return out
}

func empty() {}
`)
	fn, err := FindFunc(f, "", "build")
	require.NoError(t, err)
	lit, err := FirstCompositeLitInFunc(fn)
	require.NoError(t, err)
	require.True(t, CompositeLitHasKey(lit, "ID"))
	require.False(t, CompositeLitHasKey(lit, "Title"))

	require.NoError(t, AppendKeyValueToCompositeLit(lit, "Title", "m.Title"))
	require.True(t, CompositeLitHasKey(lit, "Title"))
	out, err := Render(f)
	require.NoError(t, err)
	require.Contains(t, string(out), "Title: m.Title,")

	fnEmpty, err := FindFunc(f, "", "empty")
	require.NoError(t, err)
	_, err = FirstCompositeLitInFunc(fnEmpty)
	require.Error(t, err)
}

func TestAppendKeyValueToCompositeLit_BadExpr(t *testing.T) {
	f := parseTestFile(t, "package x\n\nfunc b() *O {\n\treturn &O{}\n}\n")
	fn, err := FindFunc(f, "", "b")
	require.NoError(t, err)
	lit, err := FirstCompositeLitInFunc(fn)
	require.NoError(t, err)
	require.Error(t, AppendKeyValueToCompositeLit(lit, "X", "not a } valid expr"))
}

func TestInsertStmtBeforeReturn(t *testing.T) {
	f := parseTestFile(t, `package x

func (p P) AsMap() map[string]any {
	out := map[string]any{}
	if p.A != nil {
		out["a"] = *p.A
	}
	return out
}
`)
	fn, err := FindFunc(f, "P", "AsMap")
	require.NoError(t, err)
	require.True(t, FuncContainsStringLit(fn, "a"))
	require.False(t, FuncContainsStringLit(fn, "b"))

	require.NoError(t, InsertStmtBeforeReturn(fn,
		"if p.B != nil {\n\tout[\"b\"] = *p.B\n}"))
	out, err := Render(f)
	require.NoError(t, err)
	rendered := string(out)
	require.Contains(t, rendered, `out["b"] = *p.B`)
	require.Less(t, strings.Index(rendered, `out["b"]`), strings.Index(rendered, "return out"),
		"inserted statement must precede the return")
}

func TestInsertStmtBeforeReturn_NoReturnAppends(t *testing.T) {
	f := parseTestFile(t, "package x\n\nfunc side() {\n\t_ = 1\n}\n")
	fn, err := FindFunc(f, "", "side")
	require.NoError(t, err)
	require.NoError(t, InsertStmtBeforeReturn(fn, "_ = 2"))
	out, err := Render(f)
	require.NoError(t, err)
	require.Contains(t, string(out), "_ = 2")
}

func TestInsertStmtBeforeReturn_BadStmt(t *testing.T) {
	f := parseTestFile(t, "package x\n\nfunc side() {}\n")
	fn, err := FindFunc(f, "", "side")
	require.NoError(t, err)
	require.Error(t, InsertStmtBeforeReturn(fn, "if {"))
}

func TestFindVarCompositeLit_SkipsNonVarDecls(t *testing.T) {
	// Func, const and type decls before the target var exercise the
	// outer skip: only VAR GenDecls are candidates.
	f := parseTestFile(t, `package x

func F() {}

const c = 1

type T int

var cols = []string{
	"id",
}
`)
	lit, err := FindVarCompositeLit(f, "cols")
	require.NoError(t, err)
	require.Len(t, lit.Elts, 1)
}

func TestFindVarCompositeLit_NonValueSpecSkipped(t *testing.T) {
	// The parser only ever puts ValueSpecs inside a VAR GenDecl; the
	// inner guard exists for hand-built (malformed) ASTs. Build one
	// directly to prove the guard skips it instead of panicking.
	f := &File{
		Path: "x.go",
		Dst: &dst.File{
			Name: dst.NewIdent("x"),
			Decls: []dst.Decl{&dst.GenDecl{
				Tok: token.VAR,
				Specs: []dst.Spec{&dst.TypeSpec{
					Name: dst.NewIdent("T"),
					Type: dst.NewIdent("int"),
				}},
			}},
		},
	}
	_, err := FindVarCompositeLit(f, "cols")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no composite-literal var cols")
}

func TestCompositeLitHasKey_SkipsNonKeyValueElements(t *testing.T) {
	// A positional (non key:value) literal must report false for any
	// key rather than tripping on the element type.
	f := parseTestFile(t, "package x\n\nvar cols = []string{\n\t\"id\",\n}\n")
	lit, err := FindVarCompositeLit(f, "cols")
	require.NoError(t, err)
	require.False(t, CompositeLitHasKey(lit, "id"))
}

func TestInsertStmtBeforeReturn_CommentOnlyStmtSrc(t *testing.T) {
	// Parses cleanly but yields zero statements — the empty-extraction
	// error, not a silent no-op.
	f := parseTestFile(t, "package x\n\nfunc side() {}\n")
	fn, err := FindFunc(f, "", "side")
	require.NoError(t, err)
	err = InsertStmtBeforeReturn(fn, "// no statements here")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no statements extracted")
}
