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

func setupModelOnlyProject(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"),
		[]byte("database:\n  driver: postgres\n"), 0o644))

	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"), []byte(`package models

import "github.com/google/uuid"

// Order is the customer order entity.
type Order struct {
	ID    uuid.UUID `+"`gorm:\"primaryKey\"`"+`
	Total int       `+"`gorm:\"not null\"`"+`
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))

	return tmp
}

func TestGenField_AddsModelFieldAndMigration(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)

	fields := ParseFields([]string{"archive_reason:string"})
	require.Equal(t, 1, len(fields))

	require.NoError(t, GenField(FieldData{
		Resource: "Order",
		Field:    fields[0],
	}))

	model, err := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.NoError(t, err)
	require.Contains(t, string(model), "ArchiveReason")
	require.Contains(t, string(model), "// Order is the customer order entity.",
		"doc comment must be preserved by the dst round-trip")

	entries, err := os.ReadDir(filepath.Join(tmp, "db", "migrations"))
	require.NoError(t, err)
	upFound, downFound := false, false
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".up.sql"):
			upFound = true
		case strings.HasSuffix(e.Name(), ".down.sql"):
			downFound = true
		}
	}
	require.True(t, upFound, "expected an .up.sql migration to be created")
	require.True(t, downFound, "expected a .down.sql migration to be created")
}

func TestGenField_FieldAlreadyExists(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)

	fields := ParseFields([]string{"total:int"}) // already on the model
	require.Equal(t, 1, len(fields))

	err := GenField(FieldData{
		Resource: "Order",
		Field:    fields[0],
	})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeFieldAlreadyExists), ce.Code)
}

func TestGenField_DryRunRecordsPlannedActions(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)

	fields := ParseFields([]string{"notes:text"})
	SetDryRun(true)
	defer SetDryRun(false)
	require.NoError(t, GenField(FieldData{Resource: "Order", Field: fields[0]}))

	plan := Plan()
	require.GreaterOrEqual(t, len(plan), 3, "expected model patch + 2 migrations in plan")

	// One patch on the model file, one create per migration file.
	patches, creates := 0, 0
	for _, a := range plan {
		switch a.Kind {
		case "patch":
			patches++
		case "create":
			creates++
		}
	}
	require.Equal(t, 1, patches)
	require.Equal(t, 2, creates)

	// Disk must be untouched.
	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.NotContains(t, string(model), "Notes string")
}
