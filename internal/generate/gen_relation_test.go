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

func setupRelationProject(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"),
		[]byte("database:\n  driver: postgres\n"), 0o644))

	mustWriteFile(t, filepath.Join(tmp, "app", "models", "order.model.go"), `package models

import "github.com/google/uuid"

// Order is the customer order entity.
type Order struct {
	ID uuid.UUID `+"`gorm:\"primaryKey\"`"+`
}
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "models", "customer.model.go"), `package models

import "github.com/google/uuid"

// Customer is the customer entity.
type Customer struct {
	ID uuid.UUID `+"`gorm:\"primaryKey\"`"+`
}
`)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))
	return tmp
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
