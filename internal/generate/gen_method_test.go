package generate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func setupScaffoldedResource(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "order_service.go"), []byte(`package interfaces

import "context"

// OrderServiceInterface is the order business-logic contract.
type OrderServiceInterface interface {
	// Create persists a new order.
	Create(ctx context.Context, name string) error
}
`), 0o644))

	implDir := filepath.Join(tmp, "app", "services")
	require.NoError(t, os.MkdirAll(implDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(implDir, "order.service.go"), []byte(`package services

import "context"

type orderService struct{}

func (s *orderService) Create(ctx context.Context, name string) error {
	return nil
}
`), 0o644))

	return tmp
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
	require.Contains(t, string(impl), "func (s *orderService) Archive(ctx context.Context) error")
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
