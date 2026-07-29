package generate

import (
	"bytes"
	"errors"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenMock_NoModulePath(t *testing.T) {
	chdirTest(t, t.TempDir()) // no go.mod
	err := GenMock("X", GenMockOpts{})
	require.Error(t, err)
}

func TestGenMock_MissingInterfaceName(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	chdirTest(t, tmp)
	err := GenMock("", GenMockOpts{})
	require.Error(t, err)
}

func TestGenMock_AllMode_NoInterfaces(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	chdirTest(t, tmp)
	err := GenMock("", GenMockOpts{All: true})
	require.Error(t, err)
}

// — regenAllMocks: skip-on-readdir-error + skip-on-parse-error + skip
//
//	_test.go + skip directory entries.
func TestRegenAllMocks_SkipsTestFilesAndSubdirs(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	// Subdirectory.
	require.NoError(t, os.MkdirAll(filepath.Join(ifaceDir, "subdir"), 0o755))
	// _test.go file.
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "x_test.go"),
		[]byte("package interfaces\n"), 0o644))
	// Unparseable .go file.
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "broken.go"),
		[]byte("package interfaces\nfunc {\n"), 0o644))
	// One real interface so anyHit becomes true.
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "one.go"),
		[]byte("package interfaces\ntype One interface{ A() error }\n"), 0o644))
	chdirTest(t, tmp)
	require.NoError(t, GenMock("", GenMockOpts{All: true}))
}

func TestFindInterface_Ambiguous(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	svcDir := filepath.Join(tmp, "app", "services", "interfaces")
	repoDir := filepath.Join(tmp, "app", "repositories", "interfaces")
	require.NoError(t, os.MkdirAll(svcDir, 0o755))
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(svcDir, "a.go"),
		[]byte("package interfaces\ntype Same interface{ A() error }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "b.go"),
		[]byte("package interfaces\ntype Same interface{ B() error }\n"), 0o644))
	chdirTest(t, tmp)
	err := GenMock("Same", GenMockOpts{})
	require.Error(t, err)
}

func TestFindInterface_SkipsNoise(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ifaceDir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "README.md"), []byte("notes"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "x_test.go"),
		[]byte("package interfaces\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "bad.go"),
		[]byte("package interfaces\nfunc {\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "real.go"),
		[]byte("package interfaces\ntype Target interface{ A() error }\n"), 0o644))
	chdirTest(t, tmp)
	require.NoError(t, GenMock("Target", GenMockOpts{}))
}

// — scanFileForInterfaces: TypeSpec that isn't an InterfaceType is
//
//	skipped; GenDecl that isn't a type decl is skipped.
func TestScanFileForInterfaces_SkipsNonInterfaceDecls(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mixed.go")
	require.NoError(t, os.WriteFile(path, []byte(`package x
const C = 1                // not a type decl
type Alias = int           // TypeSpec but not InterfaceType
type Real interface{ F() } // hit
`), 0o644))
	targets, err := scanFileForInterfaces(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(targets))
	require.Equal(t, "Real", targets[0].Name)
}

func TestCollectFileImports_AliasedImport(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nimport ifa \"fmt\"\nvar _ = ifa.Sprintf\n"), 0o644))
	targets, err := scanFileForInterfaces(path)
	require.NoError(t, err)
	require.Equal(t, 0, len(targets))
}

func TestBuildInterfaceTarget_SkipsEmbeddedInterface(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(path, []byte(`package x
type Inner interface{ A() }
type Outer interface {
	Inner    // embedded — must skip
	B() error
}
`), 0o644))
	targets, err := scanFileForInterfaces(path)
	require.NoError(t, err)
	for _, tg := range targets {
		if tg.Name == "Outer" {
			require.Equal(t, 1, len(tg.Methods))
			require.Equal(t, "B", tg.Methods[0].Name)
		}
	}
}

func TestFlattenFuncFieldList_UnnamedParam(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(path, []byte(`package x
type Svc interface {
	F(int, string) error          // unnamed params — exercises len(Names)==0 branch
	G(a, b int) (string, error)   // multi-name grouped param
}
`), 0o644))
	targets, err := scanFileForInterfaces(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(targets))
	require.Equal(t, 2, len(targets[0].Methods))
}

func TestWriteMockForTarget_CheckMissingFile(t *testing.T) {
	src := `package interfaces
type CheckedSvc interface{ F() error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	// Don't write the mock first; --check should error with drift.
	err := GenMock("CheckedSvc", GenMockOpts{Check: true})
	require.Error(t, err)
}

func TestWriteMockForTarget_DryRunRecords(t *testing.T) {
	src := `package interfaces
type DrySvc interface{ F() error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(src), 0o644))
	SetDryRun(true)
	defer SetDryRun(false)
	require.NoError(t, GenMock("DrySvc", GenMockOpts{}))
	// File must NOT be on disk.
	_, err := os.Stat(filepath.Join(tmp, "testutil", "mocks", "dry_svc_mock.go"))
	require.True(t, os.IsNotExist(err))
}

func TestWriteMockForTarget_MkdirError(t *testing.T) {
	src := `package interfaces
type MkSvc interface{ F() error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	// Make testutil a file so MkdirAll("testutil/mocks") fails.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "testutil"), []byte("not a dir"), 0o644))
	err := GenMock("MkSvc", GenMockOpts{})
	require.Error(t, err)
}

func TestWriteMockForTarget_WriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	src := `package interfaces
type WriteSvc interface{ F() error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	mocksDir := filepath.Join(tmp, "testutil", "mocks")
	require.NoError(t, os.MkdirAll(mocksDir, 0o755))
	outPath := filepath.Join(mocksDir, "write_svc_mock.go")
	require.NoError(t, os.WriteFile(outPath, []byte("existing"), 0o644))
	require.NoError(t, os.Chmod(outPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(outPath, 0o644) })
	err := GenMock("WriteSvc", GenMockOpts{})
	require.Error(t, err)
}

func TestEmitMockMethod_NoReturnsBranch(t *testing.T) {
	src := `package interfaces
type Void interface { Do(x int) }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(src), 0o644))
	require.NoError(t, GenMock("Void", GenMockOpts{}))
	body, err := os.ReadFile(filepath.Join(tmp, "testutil", "mocks", "void_mock.go"))
	require.NoError(t, err)
	require.Contains(t, string(body), "m.Called(x)")
	require.NotContains(t, string(body), "return args")
}

func TestMockReturnAccessor_AllPrimitiveBranches(t *testing.T) {
	require.Equal(t, "args.Error(0)", mockReturnAccessor(0, "error"))
	require.Equal(t, "args.String(1)", mockReturnAccessor(1, "string"))
	require.Equal(t, "args.Int(2)", mockReturnAccessor(2, "int"))
	require.Equal(t, "args.Bool(3)", mockReturnAccessor(3, "bool"))
	require.Contains(t, mockReturnAccessor(0, "*Foo"), "v.(*Foo)")
	require.Contains(t, mockReturnAccessor(0, "[]string"), "v.([]string)")
	require.Contains(t, mockReturnAccessor(0, "map[string]int"), "v.(map[string]int)")
	require.Contains(t, mockReturnAccessor(0, "pkg.Type"), "v.(pkg.Type)")
	require.Equal(t, "args.Get(0).(NoMatch)", mockReturnAccessor(0, "NoMatch"))
}

func TestExprString_HappyPath(t *testing.T) {
	got := exprString(&ast.Ident{Name: "X"})
	require.Equal(t, "X", got)
}

func TestExprString_FormatErrorFallback(t *testing.T) {
	saved := formatNodeFn
	formatNodeFn = func(_ io.Writer, _ *token.FileSet, _ interface{}) error {
		return errStubGenerate
	}
	t.Cleanup(func() { formatNodeFn = saved })
	got := exprString(&ast.Ident{Name: "X"})
	require.NotEmpty(t, got)
}

func TestReadModulePathForMock_Missing(t *testing.T) {
	chdirTest(t, t.TempDir())
	_, err := readModulePathForMock()
	require.Error(t, err)
}

func TestDeriveImportPath_RootDir(t *testing.T) {
	require.Equal(t, "example.com/m", deriveImportPath("example.com/m", "."))
}

func TestDeriveImportPath_NormalDir(t *testing.T) {
	require.Equal(t, "example.com/m/app/services", deriveImportPath("example.com/m", "app/services"))
}

func TestRenderMock_BareImport(t *testing.T) {
	// A bare (unaliased) import referenced by a method signature renders
	// without an alias; imports the signatures never use are PRUNED
	// (goimports pass) — the interface's source file routinely imports
	// packages for its other declarations.
	body := renderMock(MockData{
		Interface:     "I",
		PackageImport: "ex/m/p",
		PackageAlias:  "p",
		ExtraImports:  []MockImport{{Path: "fmt"}, {Path: "errors"}},
		Methods: []MockMethod{{
			Name:    "F",
			Returns: []MockParam{{Type: "fmt.Stringer"}},
		}},
	})
	require.Contains(t, string(body), `"fmt"`)
	require.NotContains(t, string(body), `"errors"`, "unused source-file imports must be pruned")
}

func TestScanFileForInterfaces_SkipsNonGenDecl(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(path, []byte(`package x
func Helper() {}                // FuncDecl — not GenDecl
type Real interface{ F() }
`), 0o644))
	targets, err := scanFileForInterfaces(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(targets))
}

func TestWriteMockForTarget_CheckHappyPath(t *testing.T) {
	src := `package interfaces
type SteadySvc interface{ F() error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(src), 0o644))
	require.NoError(t, GenMock("SteadySvc", GenMockOpts{}))
	// Second call in --check mode should pass without drift.
	require.NoError(t, GenMock("SteadySvc", GenMockOpts{Check: true}))
}

func TestWriteMockForTarget_SelfImportSkipped(t *testing.T) {
	// Interface file that explicitly imports its own package path.
	// (Unusual but valid Go — exercises the `if imp.Path == d.PackageImport { continue }`
	// branch in writeMockForTarget.)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "self.go"),
		[]byte(`package interfaces
import _ "example.com/m/app/services/interfaces"
type SelfImporting interface{ F() error }
`), 0o644))
	chdirTest(t, tmp)
	require.NoError(t, GenMock("SelfImporting", GenMockOpts{}))
}

func TestRegenAllMocks_WriteMockErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "one.go"),
		[]byte("package interfaces\ntype One interface{ A() error }\n"), 0o644))
	// Block the output dir creation by placing a file there.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "testutil"), []byte("not a dir"), 0o644))
	chdirTest(t, tmp)
	err := GenMock("", GenMockOpts{All: true})
	require.Error(t, err)
}

func TestEmitMockMethod_UnnamedParamFallsBackToArgN(t *testing.T) {
	src := `package interfaces
type Unnamed interface { F(int, string) error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(src), 0o644))
	require.NoError(t, GenMock("Unnamed", GenMockOpts{}))
	body, err := os.ReadFile(filepath.Join(tmp, "testutil", "mocks", "unnamed_mock.go"))
	require.NoError(t, err)
	require.Contains(t, string(body), "arg0")
	require.Contains(t, string(body), "arg1")
}

// — renderMock: format.Source error → return raw bytes. Trigger by
//
//	producing a body that's invalid Go (impossible via normal path
//	but we can construct directly via emitMockMethod with bogus types).
func TestRenderMock_FormatFallsBackOnInvalidSource(t *testing.T) {
	// Method with an invalid identifier in the return type forces
	// format.Source to fail; renderMock returns the unformatted bytes.
	body := renderMock(MockData{
		Interface:     "I",
		PackageImport: "ex/m/p",
		PackageAlias:  "p",
		Methods: []MockMethod{
			{Name: "Bad", Returns: []MockParam{{Type: "int)}{("}}},
		},
	})
	require.NotEmpty(t, body)
	// Doc-comment marker still present even though gofmt failed.
	require.True(t, bytes.HasPrefix(body, []byte("// Code generated")))
}

func TestBuildInterfaceTarget_QualifiesPackageLocalTypes(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "iface.go")
	require.NoError(t, os.WriteFile(path, []byte(`package interfaces
import "context"

type Filters struct{ Channel string }
type Attachment struct{ Name string }

type MetricsSvc interface {
	Summary(ctx context.Context, f Filters) ([]*Attachment, error)
	ByNames(ctx context.Context, names []string, byKey map[string]Filters) (*Attachment, error)
	Stream(ctx context.Context, ch chan Filters, fn func(Filters) (*Attachment, error)) error
	Variadic(ctx context.Context, fs ...Filters) error
}
`), 0o644))
	targets, err := scanFileForInterfaces(path)
	require.NoError(t, err)
	require.Equal(t, 1, len(targets))

	m := targets[0].Methods[0] // Summary
	require.Equal(t, "interfaces.Filters", m.Params[1].Type)
	require.Equal(t, "[]*interfaces.Attachment", m.Returns[0].Type)
	require.Equal(t, "error", m.Returns[1].Type, "predeclared types stay bare")

	m = targets[0].Methods[1] // ByNames
	require.Equal(t, "[]string", m.Params[1].Type, "builtin element types stay bare")
	require.Equal(t, "map[string]interfaces.Filters", m.Params[2].Type)
	require.Equal(t, "*interfaces.Attachment", m.Returns[0].Type)

	m = targets[0].Methods[2] // Stream
	require.Equal(t, "chan interfaces.Filters", m.Params[1].Type)
	require.Equal(t, "func(interfaces.Filters) (*interfaces.Attachment, error)", m.Params[2].Type)

	m = targets[0].Methods[3] // Variadic
	require.Equal(t, "...interfaces.Filters", m.Params[1].Type)

	// Cross-package references stay verbatim.
	require.Equal(t, "context.Context", targets[0].Methods[0].Params[0].Type)
}

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

func TestGenMock_BasicInterfaceEndToEnd(t *testing.T) {
	src := `package interfaces

import "context"

type ThingService interface {
	Hello(ctx context.Context, name string) (string, error)
	Goodbye(ctx context.Context)
}
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)

	require.NoError(t, GenMock("ThingService", GenMockOpts{}))

	body, err := os.ReadFile(filepath.Join(tmp, "testutil", "mocks", "thing_service_mock.go"))
	require.NoError(t, err)

	s := string(body)
	require.Contains(t, s, "type ThingServiceMock struct")
	require.Contains(t, s, "var _ interfaces.ThingService = (*ThingServiceMock)(nil)")
	require.Contains(t, s, "func (m *ThingServiceMock) Hello(")
	require.Contains(t, s, "func (m *ThingServiceMock) Goodbye(")
	require.Contains(t, s, "args.Error(1)")
	// gofmt'd output starts with the doc comment line.
	require.True(t, strings.HasPrefix(s, "// Code generated"))
}

func TestGenMock_MissingInterfaceFires(t *testing.T) {
	src := `package interfaces

type Other interface{}
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)

	err := GenMock("ThingService", GenMockOpts{})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeInterfaceNotFound), ce.Code)
}

func TestGenMock_CheckModeDetectsDrift(t *testing.T) {
	src := `package interfaces

type ThingService interface {
	Hello() error
}
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)

	// Generate once so the file exists.
	require.NoError(t, GenMock("ThingService", GenMockOpts{}))

	// Mutate the interface so the regenerated mock would differ.
	newSrc := `package interfaces

type ThingService interface {
	Hello() error
	NewMethod() error
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(newSrc), 0o644))

	err := GenMock("ThingService", GenMockOpts{Check: true})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeMockDrift), ce.Code)
}

func TestGenMock_AllModeRegeneratesEveryInterface(t *testing.T) {
	tmp := setupMockProject(t, `package interfaces

type One interface{ A() error }
type Two interface{ B() error }
`)
	chdirTest(t, tmp)

	require.NoError(t, GenMock("", GenMockOpts{All: true}))

	for _, n := range []string{"one_mock.go", "two_mock.go"} {
		_, err := os.Stat(filepath.Join(tmp, "testutil", "mocks", n))
		require.NoError(t, err, "expected %s to be generated", n)
	}
}

// TestRenderMock_OutputIsGofmtClean asserts the generator emits already-
// formatted code — a regression here would force users to run gofmt after
// every regeneration.
func TestRenderMock_OutputIsGofmtClean(t *testing.T) {
	tmp := setupMockProject(t, `package interfaces

import "context"

type Svc interface {
	Do(ctx context.Context, n int) (string, error)
}
`)
	chdirTest(t, tmp)
	require.NoError(t, GenMock("Svc", GenMockOpts{}))

	body, err := os.ReadFile(filepath.Join(tmp, "testutil", "mocks", "svc_mock.go"))
	require.NoError(t, err)

	formatted, err := format.Source(body)
	require.NoError(t, err)
	require.True(t, bytes.Equal(body, formatted),
		"generated mock is not gofmt-clean — diff is non-empty after format.Source")
}

func TestToMockSnake(t *testing.T) {
	cases := map[string]string{
		"OrderService":         "order_service",
		"UserRepositoryReader": "user_repository_reader",
		"X":                    "x",
		"xyz":                  "xyz",
	}
	for in, want := range cases {
		if got := toMockSnake(in); got != want {
			t.Errorf("toMockSnake(%q) = %q, want %q", in, got, want)
		}
	}
}
