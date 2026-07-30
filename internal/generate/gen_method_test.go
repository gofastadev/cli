package generate

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
	"github.com/stretchr/testify/require"
)

// TestGenMethod_MissingImplFileErrors — interface exists, impl missing.
func TestGenMethod_MissingImplFileErrors(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "order_service.go"), []byte(`package interfaces
import "context"
type OrderServiceInterface interface { F(ctx context.Context) error }
`), 0o644))
	// Note: no impl file
	chdirTest(t, tmp)
	err := GenMethod(MethodData{Resource: "Order", MethodName: "X"})
	require.Error(t, err)
}

// TestGenMethod_InterfaceParseError — interface file unparseable.
func TestGenMethod_InterfaceParseError(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "order_service.go"),
		[]byte("package interfaces\nfunc {\n"), 0o644))
	implDir := filepath.Join(tmp, "app", "services")
	require.NoError(t, os.MkdirAll(implDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(implDir, "order.service.go"),
		[]byte("package services\n"), 0o644))
	chdirTest(t, tmp)
	err := GenMethod(MethodData{Resource: "Order", MethodName: "X"})
	require.Error(t, err)
}

// TestGenMethod_InterfaceNotFound — interface file parses but doesn't
// have the expected interface.
func TestGenMethod_InterfaceNotFound(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "order_service.go"),
		[]byte("package interfaces\n// no OrderServiceInterface\n"), 0o644))
	implDir := filepath.Join(tmp, "app", "services")
	require.NoError(t, os.MkdirAll(implDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(implDir, "order.service.go"),
		[]byte("package services\n"), 0o644))
	chdirTest(t, tmp)
	err := GenMethod(MethodData{Resource: "Order", MethodName: "X"})
	require.Error(t, err)
}

// TestGenMethod_AppendInterfaceMethodError — pass an Arg with a bad
// GoType that breaks the synthetic wrap.
func TestGenMethod_AppendInterfaceMethodError(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	err := GenMethod(MethodData{
		Resource:   "Order",
		MethodName: "X",
		Args:       []Field{{Name: "bad", GoType: "int }`broken"}},
	})
	require.Error(t, err)
}

// TestGenMethod_InterfaceWriteBackError — chmod the iface file readonly.
func TestGenMethod_InterfaceWriteBackError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	ifacePath := filepath.Join(tmp, "app", "services", "interfaces", "order_service.go")
	require.NoError(t, os.Chmod(ifacePath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(ifacePath, 0o644) })
	err := GenMethod(MethodData{Resource: "Order", MethodName: "X"})
	require.Error(t, err)
}

// TestGenMethod_ImplParseError — impl file unparseable.
func TestGenMethod_ImplParseError(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	implPath := filepath.Join(tmp, "app", "services", "order.service.go")
	require.NoError(t, os.WriteFile(implPath, []byte("package services\nfunc {\n"), 0o644))
	err := GenMethod(MethodData{Resource: "Order", MethodName: "X"})
	require.Error(t, err)
}

// TestGenMethod_AppendFuncDeclError — set ImplStructName to a
// non-identifier so the iface side passes (it uses InterfaceName) but
// the impl-side AppendFuncDecl fails when wrapping receiver `(s *bad{)`.
func TestGenMethod_AppendFuncDeclError(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	err := GenMethod(MethodData{
		Resource:       "Order",
		MethodName:     "Valid",
		InterfaceName:  "OrderServiceInterface",
		ImplStructName: "bad{",
		InterfaceFile:  filepath.Join("app", "services", "interfaces", "order_service.go"),
		ImplFile:       filepath.Join("app", "services", "order.service.go"),
	})
	require.Error(t, err)
}

// TestWriteBackOrRecord_RenderError — inject a Render failure via the
// astpatchRenderFn seam.
func TestWriteBackOrRecord_RenderError(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(srcPath, []byte("package x\n"), 0o644))
	f, err := astpatch.Parse(srcPath)
	require.NoError(t, err)

	saved := astpatchRenderFn
	astpatchRenderFn = func(_ *astpatch.File) ([]byte, error) {
		return nil, errStubGenerate
	}
	t.Cleanup(func() { astpatchRenderFn = saved })

	require.Error(t, writeBackOrRecord(f, "noop"))
}

func TestGenMethod_AppendsToInterfaceAndImpl(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenMethod(MethodData{Resource: "Order", MethodName: "Archive"}))

	iface, err := os.ReadFile(filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"))
	require.NoError(t, err)
	require.Contains(t, string(iface), "Archive(ctx context.Context) error")
	require.Contains(t, string(iface), "// OrderServiceInterface is the order business-logic contract.")

	impl, err := os.ReadFile(filepath.Join(tmp, "app", "services", "order.service.go"))
	require.NoError(t, err)
	// Default receiver matches the scaffold's exported `type OrderService`.
	require.Contains(t, string(impl), "func (s *OrderService) Archive(ctx context.Context) error")
	require.Contains(t, string(impl), "\"fmt\"")
	// The patched file must remain valid Go (single-line import gained
	// a second spec — the astpatch parenthesization regression).
	_, perr := parser.ParseFile(token.NewFileSet(), "order.service.go", impl, 0)
	require.NoError(t, perr, "patched impl must parse:\n%s", impl)
}

// TestGenMethod_ArgTypeImports — uuid/time args must pull their imports
// into BOTH the interface and impl files.
func TestGenMethod_ArgTypeImports(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenMethod(MethodData{
		Resource:   "Order",
		MethodName: "Reschedule",
		Args: []Field{
			{Name: "OwnerID", GoType: "uuid.UUID"},
			{Name: "DueAt", GoType: "time.Time"},
		},
	}))

	for _, rel := range []string{
		filepath.Join("app", "services", "interfaces", "order_service.go"),
		filepath.Join("app", "services", "order.service.go"),
	} {
		body, err := os.ReadFile(filepath.Join(tmp, rel))
		require.NoError(t, err)
		require.Contains(t, string(body), "\"github.com/google/uuid\"", rel)
		require.Contains(t, string(body), "\"time\"", rel)
		_, perr := parser.ParseFile(token.NewFileSet(), rel, body, 0)
		require.NoError(t, perr, "%s must parse:\n%s", rel, body)
	}
}

func TestGenMethod_IdempotencyCheck(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenMethod(MethodData{Resource: "Order", MethodName: "Archive"}))

	err := GenMethod(MethodData{Resource: "Order", MethodName: "Archive"})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeMethodAlreadyExists), ce.Code)
}

func TestGenMethod_WithArgsBuildsSignature(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	args := ParseFields([]string{"reason:string", "force:bool"})
	require.NoError(t, GenMethod(MethodData{
		Resource:   "Order",
		MethodName: "Cancel",
		Args:       args,
	}))

	iface, _ := os.ReadFile(filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"))
	require.Contains(t, string(iface), "reason string")
	require.Contains(t, string(iface), "force bool")
}

func TestGenMethod_MissingResourceFires(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	chdirTest(t, tmp)

	err := GenMethod(MethodData{Resource: "Ghost", MethodName: "Vanish"})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeResourceNotFound), ce.Code)
}

func TestGenMethod_DryRunRecordsPatchesOnly(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	SetDryRun(true)
	defer SetDryRun(false)
	require.NoError(t, GenMethod(MethodData{Resource: "Order", MethodName: "DryArchive"}))

	plan := Plan()
	require.Equal(t, 2, len(plan), "expected interface + impl patches")
	for _, a := range plan {
		require.Equal(t, "patch", a.Kind)
		require.True(t,
			strings.HasSuffix(a.Path, "order_service.go") ||
				strings.HasSuffix(a.Path, "order.service.go"),
			"unexpected path: %s", a.Path)
	}

	// Disk must be unchanged in dry-run mode.
	body, _ := os.ReadFile(filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"))
	require.NotContains(t, string(body), "DryArchive")
}

func TestZeroValueFor(t *testing.T) {
	cases := map[string]string{
		"*models.Order":   "nil",
		"[]*models.Order": "nil",
		"map[string]int":  "nil",
		"chan int":        "nil",
		"func(int) error": "nil",
		"any":             "nil",
		"error":           "nil",
		"interface{}":     "nil",
		"string":          `""`,
		"bool":            "false",
		"int":             "0",
		"int64":           "0",
		"float64":         "0",
		"uuid.UUID":       "uuid.Nil",
		"time.Time":       "time.Time{}",
		"models.Order":    "models.Order{}",
	}
	for in, want := range cases {
		require.Equal(t, want, zeroValueFor(in), "zeroValueFor(%q)", in)
	}
}

// TestGenMethod_ReturnsTuple — --returns "*models.Order, error" shape:
// tuple signature on the interface, zero-value + fmt.Errorf stub body.
func TestGenMethod_ReturnsTuple(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenMethod(MethodData{
		Resource:   "Order",
		MethodName: "Reprice",
		Returns:    []string{"*models.Order", "error"},
	}))

	iface, err := os.ReadFile(filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"))
	require.NoError(t, err)
	require.Contains(t, string(iface), "Reprice(ctx context.Context) (*models.Order, error)")

	impl, err := os.ReadFile(filepath.Join(tmp, "app", "services", "order.service.go"))
	require.NoError(t, err)
	require.Contains(t, string(impl), "func (s *OrderService) Reprice(ctx context.Context) (*models.Order, error)")
	require.Contains(t, string(impl), `return nil, fmt.Errorf("OrderServiceInterface.Reprice: not implemented")`)
}

// TestGenMethod_ReturnsWithoutError — a non-error result list gets pure
// zero values and must NOT force the fmt import.
func TestGenMethod_ReturnsWithoutError(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenMethod(MethodData{
		Resource:   "Order",
		MethodName: "PendingCount",
		Returns:    []string{"int"},
	}))

	impl, err := os.ReadFile(filepath.Join(tmp, "app", "services", "order.service.go"))
	require.NoError(t, err)
	require.Contains(t, string(impl), "func (s *OrderService) PendingCount(ctx context.Context) int {")
	require.Contains(t, string(impl), "return 0")
	require.NotContains(t, string(impl), "\"fmt\"")
	_, perr := parser.ParseFile(token.NewFileSet(), "order.service.go", impl, 0)
	require.NoError(t, perr, "patched impl must parse:\n%s", impl)
}
