package astpatch

import (
	"bytes"
	"errors"
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

func TestRender_RestoreErrorOnUnformattableButValid(t *testing.T) {
	// We can't easily make decorator.Fprint fail with a valid dst.File,
	// so we exercise the format.Source error path instead: render a
	// file with deliberately broken-after-restore source. Construct a
	// dst.File with an empty Name to force the restorer to produce
	// invalid output that format.Source rejects but bytes are still
	// returned (no error).
	bad := &File{
		Path: "x.go",
		Dst: &dst.File{
			Name:  dst.NewIdent(""), // empty package name → invalid Go
			Decls: nil,
		},
	}
	body, err := Render(bad)
	// Render returns the unformatted bytes on format error — never the
	// error itself.
	require.NoError(t, err)
	require.NotNil(t, body)
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
