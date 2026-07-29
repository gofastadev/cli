package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
