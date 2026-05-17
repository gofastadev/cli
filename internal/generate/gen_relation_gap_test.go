package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// setupRelationProject already exists in gen_relation_test.go.

func TestGenRelation_ValidateError_MissingResource(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenRelation(RelationData{Kind: RelationBelongsTo})
	require.Error(t, err)
}

func TestGenRelation_InvalidKind(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenRelation(RelationData{Resource: "Order", Other: "Customer", Kind: "bogus"})
	require.Error(t, err)
}

func TestGenRelation_MissingModelFile(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	chdirTest(t, tmp)
	err := GenRelation(RelationData{Resource: "Order", Other: "Customer", Kind: RelationBelongsTo})
	require.Error(t, err)
}

func TestGenRelation_ParseError(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\nfunc {\n"), 0o644))
	chdirTest(t, tmp)
	err := GenRelation(RelationData{Resource: "Order", Other: "Customer", Kind: RelationBelongsTo})
	require.Error(t, err)
}

func TestGenRelation_StructMissing(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\n// no Order struct\n"), 0o644))
	chdirTest(t, tmp)
	err := GenRelation(RelationData{Resource: "Order", Other: "Customer", Kind: RelationBelongsTo})
	require.Error(t, err)
}

func TestGenRelation_HasMany_HappyPath(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	require.NoError(t, GenRelation(RelationData{
		Resource: "Order", Other: "LineItem", Kind: RelationHasMany,
	}))
	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.Contains(t, string(model), "[]LineItem")
}

func TestGenRelation_HasOne_HappyPath(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	require.NoError(t, GenRelation(RelationData{
		Resource: "Order", Other: "Customer", Kind: RelationHasOne,
	}))
	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.Contains(t, string(model), "*Customer")
}

func TestGenRelation_BelongsTo_AppendStructFieldError(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	// "Bad}" as Other makes the synthetic struct field unparseable.
	err := GenRelation(RelationData{
		Resource: "Order", Other: "Bad}", Kind: RelationBelongsTo,
	})
	require.Error(t, err)
}

func TestGenRelation_WriteBackError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	modelPath := filepath.Join(tmp, "app", "models", "order.model.go")
	require.NoError(t, os.Chmod(modelPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(modelPath, 0o644) })
	err := GenRelation(RelationData{
		Resource: "Order", Other: "Customer", Kind: RelationBelongsTo,
	})
	require.Error(t, err)
}

func TestRelationModelFields_UnknownKind(t *testing.T) {
	// Unknown kind → return nil branch.
	require.Nil(t, relationModelFields(RelationData{Kind: "bogus"}))
}

func TestWriteRelationMigration_FirstWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := t.TempDir()
	migDir := filepath.Join(tmp, "migs")
	require.NoError(t, os.MkdirAll(migDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(migDir, 0o755) })
	err := writeRelationMigration(RelationData{
		Resource: "Order", Other: "Customer",
		MigrationDir: migDir,
		MigrationVer: "000001",
	})
	require.Error(t, err)
}
