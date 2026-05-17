package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenRename_ValidationError_Missing — one of Resource/Old/New missing.
func TestGenRename_ValidationError_Missing(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenRename(RenameData{Resource: "Order"})
	require.Error(t, err)
}

// TestGenRename_ValidationError_SameNames — old == new.
func TestGenRename_ValidationError_SameNames(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "Total",
	})
	require.Error(t, err)
}

// TestGenRename_PreviewModeNoWrites — preview mode records actions
// without touching disk; exercises the recordPatch / recordCreate
// branches.
func TestGenRename_PreviewMode(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte(`package models
type Order struct{ Total int }
`), 0o644))
	chdirTest(t, tmp)

	resetPlannerState(t)
	require.NoError(t, GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents",
	}))
	plan := Plan()
	require.GreaterOrEqual(t, len(plan), 1)

	// Disk untouched.
	body, _ := os.ReadFile(filepath.Join(models, "order.model.go"))
	require.Contains(t, string(body), "Total")
}

// TestGenRename_ApplyWritesToDisk — Apply=true commits the rewrites.
func TestGenRename_ApplyWritesToDisk(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte(`package models
type Order struct{ Total int }
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))
	chdirTest(t, tmp)
	require.NoError(t, GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents", Apply: true,
	}))
	body, _ := os.ReadFile(filepath.Join(models, "order.model.go"))
	require.Contains(t, string(body), "AmountCents")
}

// TestGenRename_FileWithoutMatchesSkipped — file exists but doesn't
// contain the OldField; applyRenameRules returns body unchanged, the
// bytes.Equal branch fires and the loop continues without recording.
func TestGenRename_FileWithoutMatchesSkipped(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	// Model has UnrelatedField, not Total.
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\ntype Order struct{ UnrelatedField int }\n"), 0o644))
	chdirTest(t, tmp)
	resetPlannerState(t)
	require.NoError(t, GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents",
	}))
}

// TestGenRename_ApplyMigrationWriteError — make db/migrations a file
// (not a dir) so writeOrRecordCreate's mkdir fails.
func TestGenRename_ApplyMigrationWriteError(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db"), 0o755))
	// Put a regular file where the migrations dir should be.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "db", "migrations"),
		[]byte("not a dir"), 0o644))
	chdirTest(t, tmp)
	err := GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents", Apply: true,
	})
	require.Error(t, err)
}

// TestGenRename_ApplyWriteError — chmod target so os.WriteFile fails.
func TestGenRename_ApplyWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	modelPath := filepath.Join(models, "order.model.go")
	require.NoError(t, os.WriteFile(modelPath,
		[]byte("package models\ntype Order struct{ Total int }\n"), 0o644))
	require.NoError(t, os.Chmod(modelPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(modelPath, 0o644) })
	chdirTest(t, tmp)
	err := GenRename(RenameData{
		Resource: "Order", OldField: "Total", NewField: "AmountCents", Apply: true,
	})
	require.Error(t, err)
}
