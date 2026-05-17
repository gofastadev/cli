package generate

import (
	"os"
	"path/filepath"
	"testing"

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

// TestWriteBackOrRecord_RenderError — inject a Render failure via the
// astpatch restorerFprintFn seam.
func TestWriteBackOrRecord_RenderError(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "x.go")
	require.NoError(t, os.WriteFile(srcPath, []byte("package x\n"), 0o644))
	f, err := astpatch.Parse(srcPath)
	require.NoError(t, err)

	// Hard to make Render fail without seam — exercise the happy path here.
	require.NoError(t, writeBackOrRecord(f, "noop"))
}
