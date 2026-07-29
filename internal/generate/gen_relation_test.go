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

func TestGenRelation_BelongsTo_PatchesModelAndEmitsMigration(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)

	require.NoError(t, GenRelation(RelationData{
		Resource: "Order",
		Kind:     RelationBelongsTo,
		Other:    "Customer",
	}))

	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	// gofmt aligns the field columns so the spacing between name and
	// type is variable; match the substring with a permissive whitespace
	// allowance.
	require.Regexp(t, `CustomerID\s+uuid\.UUID`, string(model))
	require.Regexp(t, `\bCustomer\s+\*Customer\b`, string(model))

	// Migration pair must exist with the right columns.
	entries, _ := os.ReadDir(filepath.Join(tmp, "db", "migrations"))
	var foundUp, foundDown bool
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".up.sql"):
			body, _ := os.ReadFile(filepath.Join(tmp, "db", "migrations", e.Name()))
			require.Contains(t, string(body), "ALTER TABLE orders ADD COLUMN customer_id uuid")
			require.Contains(t, string(body), "FOREIGN KEY")
			foundUp = true
		case strings.HasSuffix(e.Name(), ".down.sql"):
			body, _ := os.ReadFile(filepath.Join(tmp, "db", "migrations", e.Name()))
			require.Contains(t, string(body), "DROP CONSTRAINT")
			foundDown = true
		}
	}
	require.True(t, foundUp && foundDown)
}

func TestGenRelation_HasMany_AddsSliceFieldOnly(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)

	require.NoError(t, GenRelation(RelationData{
		Resource: "Customer",
		Kind:     RelationHasMany,
		Other:    "Order",
	}))

	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "customer.model.go"))
	require.Contains(t, string(model), "Orders []Order")

	// No parent-side migration for has_many.
	entries, _ := os.ReadDir(filepath.Join(tmp, "db", "migrations"))
	require.Equal(t, 0, len(entries),
		"has_many must not emit a migration on the parent side")
}

func TestGenRelation_HasOne_AddsPointerFieldOnly(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)

	require.NoError(t, GenRelation(RelationData{
		Resource: "Customer",
		Kind:     RelationHasOne,
		Other:    "Order",
	}))

	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "customer.model.go"))
	require.Contains(t, string(model), "Order *Order")
}

func TestGenRelation_IdempotentSecondCall(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)

	// First call adds fields + migration.
	require.NoError(t, GenRelation(RelationData{
		Resource: "Order",
		Kind:     RelationBelongsTo,
		Other:    "Customer",
	}))
	// Second call must not crash; fields already exist so the in-model
	// patch is a no-op (StructHasField gate). A second migration WILL
	// be emitted (the file names share a version prefix so they collide
	// with the existing files — they're skipped by writeOrRecordCreate).
	require.NoError(t, GenRelation(RelationData{
		Resource: "Order",
		Kind:     RelationBelongsTo,
		Other:    "Customer",
	}))
}

func TestGenRelation_ValidationErrors(t *testing.T) {
	t.Run("empty-resource", func(t *testing.T) {
		err := validateRelation(RelationData{Other: "X", Kind: RelationBelongsTo})
		require.Error(t, err)
	})
	t.Run("empty-other", func(t *testing.T) {
		err := validateRelation(RelationData{Resource: "X", Kind: RelationBelongsTo})
		require.Error(t, err)
	})
	t.Run("bad-kind", func(t *testing.T) {
		err := validateRelation(RelationData{Resource: "X", Other: "Y", Kind: "weird"})
		require.Error(t, err)
		var ce *clierr.Error
		require.True(t, errors.As(err, &ce))
		require.Equal(t, string(clierr.CodeInvalidName), ce.Code)
	})
	t.Run("happy", func(t *testing.T) {
		require.NoError(t, validateRelation(RelationData{
			Resource: "X", Other: "Y", Kind: RelationHasOne,
		}))
	})
}

func TestGenRelation_MissingResourceModel(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	chdirTest(t, tmp)

	err := GenRelation(RelationData{
		Resource: "Ghost",
		Kind:     RelationBelongsTo,
		Other:    "Vapor",
	})
	require.Error(t, err)
}

func TestRelationModelFields_BelongsToPair(t *testing.T) {
	fields := relationModelFields(RelationData{
		Resource: "Order",
		Other:    "Customer",
		Kind:     RelationBelongsTo,
	})
	require.Equal(t, 2, len(fields))
	require.Contains(t, fields[0], "CustomerID uuid.UUID")
	require.Contains(t, fields[1], "Customer *Customer")
}
