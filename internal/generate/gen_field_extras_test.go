package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenField_PatchesDTOWhenPresent runs the full path through patchDTOFile
// — present in the file at 0% coverage before this test landed.
func TestGenField_PatchesDTOWhenPresent(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)

	// Add a DTOs file with all three variants so each branch of
	// dtoVariants is exercised + every field actually gets appended.
	mustWriteFile(t, filepath.Join(tmp, "app", "dtos", "order.dtos.go"), `package dtos

type OrderCreateRequest struct{ Total int }
type OrderUpdateRequest struct{ Total int }
type OrderResponse struct{ Total int }
`)

	fields := ParseFields([]string{"archive_reason:string"})
	require.NoError(t, GenField(FieldData{
		Resource:     "Order",
		Field:        fields[0],
		WithDTO:      true,
		WithCreate:   true,
		WithUpdate:   true,
		WithResponse: true,
	}))

	dto, _ := os.ReadFile(filepath.Join(tmp, "app", "dtos", "order.dtos.go"))
	// All three DTOs got the field.
	require.Contains(t, string(dto), "OrderCreateRequest")
	require.Contains(t, string(dto), "OrderUpdateRequest")
	require.Contains(t, string(dto), "OrderResponse")
	// And the field is present at least once.
	require.Contains(t, string(dto), "ArchiveReason")
}

func TestDtoVariants_Flags(t *testing.T) {
	require.Empty(t, dtoVariants(FieldData{Resource: "X"}))
	require.Equal(t, []string{"XCreateRequest"},
		dtoVariants(FieldData{Resource: "X", WithCreate: true}))
	require.Equal(t, []string{"XCreateRequest", "XUpdateRequest", "XResponse"},
		dtoVariants(FieldData{
			Resource: "X", WithCreate: true, WithUpdate: true, WithResponse: true,
		}))
}

func TestFileExistsHelper(t *testing.T) {
	tmp := t.TempDir()
	require.False(t, fileExistsHelper(filepath.Join(tmp, "missing")))

	path := filepath.Join(tmp, "present.txt")
	require.NoError(t, os.WriteFile(path, []byte{}, 0o644))
	require.True(t, fileExistsHelper(path))
}

func TestReadDBDriverSafe_ReadsConfigOrFallsBack(t *testing.T) {
	t.Run("fallback-postgres", func(t *testing.T) {
		chdirTest(t, t.TempDir())
		require.Equal(t, "postgres", readDBDriverSafe())
	})
	t.Run("reads-config", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"),
			[]byte("driver: mysql\n"), 0o644))
		chdirTest(t, tmp)
		require.Equal(t, "mysql", readDBDriverSafe())
	})
}
