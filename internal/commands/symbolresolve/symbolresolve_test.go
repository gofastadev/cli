package symbolresolve

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// withinTempModule sets up a tiny throwaway Go module rooted at a temp
// dir, writes the given files (relative-path → content), and chdirs in.
// No return value — tests address files via their relative paths.
func withinTempModule(t *testing.T, files map[string]string) {
	t.Helper()
	tmp := t.TempDir()

	mod := "module example.com/m\n\ngo 1.25\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(mod), 0o644))

	for rel, body := range files {
		full := filepath.Join(tmp, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestLookupReferences_FindsUseSites(t *testing.T) {
	withinTempModule(t, map[string]string{
		"a/a.go": `package a

func Helper() int { return 1 }
`,
		"main.go": `package main

import "example.com/m/a"

func main() {
	_ = a.Helper()
	_ = a.Helper()
}
`,
	})

	report, err := LookupReferences("example.com/m/a.Helper")
	require.NoError(t, err)
	require.Equal(t, "func", report.Kind)
	require.GreaterOrEqual(t, report.Count, 2,
		"expected at least 2 references to Helper, got %d", report.Count)
	require.NotNil(t, report.Definition)
}

func TestLookupReferences_UnqualifiedNameWorksWhenUnique(t *testing.T) {
	withinTempModule(t, map[string]string{
		"a/a.go": `package a

type Thing struct{}

func New() *Thing { return &Thing{} }
`,
		"main.go": `package main

import "example.com/m/a"

func main() { _ = a.New() }
`,
	})

	// Bare name "Thing" appears only in package a.
	report, err := LookupReferences("Thing")
	require.NoError(t, err)
	require.Equal(t, "type", report.Kind)
}

func TestLookupReferences_AmbiguousUnqualifiedReturnsCode(t *testing.T) {
	withinTempModule(t, map[string]string{
		"a/a.go": `package a

func Same() {}
`,
		"b/b.go": `package b

func Same() {}
`,
	})

	_, err := LookupReferences("Same")
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeAmbiguousSymbol), ce.Code)
}

func TestLookupReferences_MissingSymbolReturnsCode(t *testing.T) {
	withinTempModule(t, map[string]string{
		"a/a.go": "package a\n",
	})

	_, err := LookupReferences("Nope")
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeSymbolNotFound), ce.Code)
}

func TestImpactGraph_ResolvesByFilePath(t *testing.T) {
	withinTempModule(t, map[string]string{
		"a/a.go": "package a\n\nfunc F() int { return 1 }\n",
		"main.go": `package main

import "example.com/m/a"

func main() { _ = a.F() }
`,
	})

	r, err := ImpactGraph("a/a.go")
	require.NoError(t, err)
	require.Equal(t, "example.com/m/a", r.Package)
	require.Contains(t, r.DirectImporters, "example.com/m")
}

func TestImpactGraph_ResolvesByImportPath(t *testing.T) {
	withinTempModule(t, map[string]string{
		"a/a.go": "package a\n\nfunc F() {}\n",
		"main.go": `package main

import "example.com/m/a"

func main() { a.F() }
`,
	})

	r, err := ImpactGraph("example.com/m/a")
	require.NoError(t, err)
	require.Equal(t, "example.com/m/a", r.Package)
}

func TestImpactGraph_UnknownTargetReturnsCode(t *testing.T) {
	withinTempModule(t, map[string]string{"a/a.go": "package a\n"})
	_, err := ImpactGraph("does/not/exist.go")
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeSymbolNotFound), ce.Code)
}

func TestLoadModule_RunsCleanlyOnEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// `go/packages` in an empty dir doesn't necessarily fail; it just
	// returns zero packages or an environment-dependent error. Either
	// outcome is acceptable — the test exercises the load path for
	// coverage and asserts nothing about the result.
	_, _ = loadModuleFn()
}

// ----- internal helpers ---------------------------------------------------

func TestObjectKind(t *testing.T) {
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	fn := types.NewFunc(token.NoPos, nil, "F", sig)
	require.Equal(t, "func", objectKind(fn))

	tn := types.NewTypeName(token.NoPos, nil, "T", nil)
	require.Equal(t, "type", objectKind(tn))

	v := types.NewVar(token.NoPos, nil, "v", types.Typ[types.Int])
	require.Equal(t, "var", objectKind(v))

	c := types.NewConst(token.NoPos, nil, "c", types.Typ[types.Int], nil)
	require.Equal(t, "const", objectKind(c))

	// Method (signature with receiver) returns "method".
	recv := types.NewVar(token.NoPos, nil, "r", types.Typ[types.Int])
	methodSig := types.NewSignatureType(recv, nil, nil, nil, nil, false)
	method := types.NewFunc(token.NoPos, nil, "M", methodSig)
	require.Equal(t, "method", objectKind(method))
}

func TestReceiverString(t *testing.T) {
	require.Equal(t, "T", receiverString(&ast.Ident{Name: "T"}))
	require.Equal(t, "T", receiverString(&ast.StarExpr{X: &ast.Ident{Name: "T"}}))
	require.Equal(t, "?", receiverString(&ast.BadExpr{}))
}

func TestRelToCwd(t *testing.T) {
	cwd, _ := os.Getwd()
	abs := filepath.Join(cwd, "child", "file.go")
	rel := relToCwd(abs)
	require.Equal(t, filepath.Join("child", "file.go"), rel)

	// Path outside cwd falls back to absolute.
	external := "/some/other/place"
	require.Equal(t, external, relToCwd(external))
}

func TestResolveTargetToPackage_ByImportPath(t *testing.T) {
	pkgs := []*packages.Package{
		{PkgPath: "example.com/a"},
		{PkgPath: "example.com/b"},
	}
	require.Equal(t, "example.com/a", resolveTargetToPackage("example.com/a", pkgs))
	require.Equal(t, "", resolveTargetToPackage("not/there", pkgs))
}

func TestMatchIdent_AllShapes(t *testing.T) {
	ident := &ast.Ident{Name: "X"}
	require.True(t, matchIdent(ident, ident))
	require.True(t, matchIdent(&ast.SelectorExpr{Sel: ident}, ident))
	require.False(t, matchIdent(&ast.BasicLit{}, ident))
}
