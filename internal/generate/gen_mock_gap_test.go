package generate

import (
	"bytes"
	"go/ast"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// — GenMock validation + delegation branches ──────────────────────────

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

// — findInterface: ambiguous (two files declare same name) ─────────────

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

// — findInterface: skip non-.go file + skip directory + skip parse-error.

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

// — collectFileImports: import with alias.

func TestCollectFileImports_AliasedImport(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(path,
		[]byte("package x\nimport ifa \"fmt\"\nvar _ = ifa.Sprintf\n"), 0o644))
	targets, err := scanFileForInterfaces(path)
	require.NoError(t, err)
	require.Equal(t, 0, len(targets))
}

// — buildInterfaceTarget: skips embedded interfaces (no Names).

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

// — flattenFuncFieldList: unnamed-param branch.

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

// — writeMockForTarget: --check path with missing file.

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

// — writeMockForTarget: dry-run records create.

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

// — writeMockForTarget: mkdir failure when parent is a file.

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

// — writeMockForTarget: write failure via chmod read-only.

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

// — emitMockMethod: no returns + multi-return + various accessor types.

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

// — mockReturnAccessor: string / int / bool / pointer / slice / map / dotted.

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

// — exprString: error path → falls back to %v.

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

// — readModulePathForMock: when no go.mod or missing module directive.

func TestReadModulePathForMock_Missing(t *testing.T) {
	chdirTest(t, t.TempDir())
	_, err := readModulePathForMock()
	require.Error(t, err)
}

// — deriveImportPath: dir == "." returns the module path itself.

func TestDeriveImportPath_RootDir(t *testing.T) {
	require.Equal(t, "example.com/m", deriveImportPath("example.com/m", "."))
}

func TestDeriveImportPath_NormalDir(t *testing.T) {
	require.Equal(t, "example.com/m/app/services", deriveImportPath("example.com/m", "app/services"))
}

// — renderMock: ensure imports with no alias use bare-string form.

func TestRenderMock_BareImport(t *testing.T) {
	body := renderMock(MockData{
		Interface:     "I",
		PackageImport: "ex/m/p",
		PackageAlias:  "p",
		ExtraImports:  []MockImport{{Path: "fmt"}},
		Methods:       []MockMethod{{Name: "F"}},
	})
	require.Contains(t, string(body), `"fmt"`)
}

// — scanFileForInterfaces: non-GenDecl skipped (line 217-218).

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

// — writeMockForTarget: --check happy path (existing mock matches) ─────

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

// — writeMockForTarget: imp.Path == d.PackageImport skip branch ──────

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

// — regenAllMocks: writeMockForTarget error propagates ─────────────────

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

// — emitMockMethod: unnamed param (Name == "") via end-to-end GenMock.

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
