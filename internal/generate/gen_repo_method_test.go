package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupScaffoldedRepo(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	ifaceDir := filepath.Join(tmp, "app", "repositories", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "order_repository.go"),
		[]byte(`package interfaces

import "context"

// OrderRepositoryInterface persists Order rows.
type OrderRepositoryInterface interface {
	Create(ctx context.Context, name string) error
}
`), 0o644))

	implDir := filepath.Join(tmp, "app", "repositories")
	require.NoError(t, os.MkdirAll(implDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(implDir, "order.repository.go"),
		[]byte(`package repositories

import "context"

type orderRepository struct{}

func (r *orderRepository) Create(ctx context.Context, name string) error {
	return nil
}
`), 0o644))
	return tmp
}

func TestGenRepoMethod_HappyPath(t *testing.T) {
	tmp := setupScaffoldedRepo(t)
	chdirTest(t, tmp)

	require.NoError(t, GenRepoMethod(MethodData{Resource: "Order", MethodName: "Archive"}))

	iface, err := os.ReadFile(filepath.Join(tmp, "app", "repositories", "interfaces", "order_repository.go"))
	require.NoError(t, err)
	require.Contains(t, string(iface), "Archive(ctx context.Context) error")

	impl, err := os.ReadFile(filepath.Join(tmp, "app", "repositories", "order.repository.go"))
	require.NoError(t, err)
	require.Contains(t, string(impl), "*orderRepository) Archive(ctx context.Context) error")
}

// TestGenRepoMethod_EmptyResourceDelegates — empty Resource short-
// circuits to GenMethod (which surfaces its own missing-name error).
func TestGenRepoMethod_EmptyResourceDelegates(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	err := GenRepoMethod(MethodData{Resource: "", MethodName: "X"})
	require.Error(t, err)
}

// TestGenRepoMethod_HonorsCallerOverrides — when InterfaceName /
// ImplStructName / files are already set on the input, GenRepoMethod
// must not overwrite them.
func TestGenRepoMethod_HonorsCallerOverrides(t *testing.T) {
	tmp := setupScaffoldedRepo(t)
	chdirTest(t, tmp)

	require.NoError(t, GenRepoMethod(MethodData{
		Resource:       "Order",
		MethodName:     "ArchiveExplicit",
		InterfaceName:  "OrderRepositoryInterface",
		ImplStructName: "orderRepository",
		InterfaceFile:  filepath.Join("app", "repositories", "interfaces", "order_repository.go"),
		ImplFile:       filepath.Join("app", "repositories", "order.repository.go"),
	}))
}
