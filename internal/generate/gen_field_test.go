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

// TestGenField_MissingResourceErrors — ensureExists(d.ModelFile) fails
// when the model file doesn't exist.
func TestGenField_MissingResourceErrors(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	chdirTest(t, tmp)
	err := GenField(FieldData{
		Resource: "Ghost",
		Field:    ParseFields([]string{"x:string"})[0],
	})
	require.Error(t, err)
}

// TestGenField_ParseError — model file has invalid Go source.
func TestGenField_ParseError(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\nfunc {\n"), 0o644))
	chdirTest(t, tmp)
	err := GenField(FieldData{
		Resource: "Order",
		Field:    ParseFields([]string{"x:string"})[0],
	})
	require.Error(t, err)
}

// TestGenField_StructMissing — model file parses but has no struct
// matching Resource.
func TestGenField_StructMissing(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\n// no Order struct\n"), 0o644))
	chdirTest(t, tmp)
	err := GenField(FieldData{
		Resource: "Order",
		Field:    ParseFields([]string{"x:string"})[0],
	})
	require.Error(t, err)
}

// TestGenField_TimeImportAdded — Field has GoType time.Time → EnsureImport.
func TestGenField_TimeImportAdded(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	require.NoError(t, GenField(FieldData{
		Resource: "Order",
		Field:    ParseFields([]string{"shipped_at:time"})[0],
	}))
	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.Contains(t, string(model), `"time"`)
}

// TestGenField_UUIDImportAdded — uuid:uuid.UUID type adds uuid import.
func TestGenField_UUIDImportAdded(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	require.NoError(t, GenField(FieldData{
		Resource: "Order",
		Field:    ParseFields([]string{"linked_id:uuid"})[0],
	}))
	model, _ := os.ReadFile(filepath.Join(tmp, "app", "models", "order.model.go"))
	require.Contains(t, string(model), "uuid")
}

// TestGenField_PatchDTOFile_HappyPath — DTO file exists with the
// expected variants; field is added to each.
func TestGenField_PatchDTOFile_HappyPath(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	dtos := filepath.Join(tmp, "app", "dtos")
	require.NoError(t, os.MkdirAll(dtos, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dtos, "order.dtos.go"),
		[]byte(`package dtos
type OrderCreateRequest struct{ ExistingField string }
type OrderUpdateRequest struct{ ExistingField string }
type OrderResponse struct{ ExistingField string }
`), 0o644))

	require.NoError(t, GenField(FieldData{
		Resource:     "Order",
		Field:        ParseFields([]string{"reason:string"})[0],
		WithDTO:      true,
		WithCreate:   true,
		WithUpdate:   true,
		WithResponse: true,
	}))
	dtosBody, _ := os.ReadFile(filepath.Join(dtos, "order.dtos.go"))
	require.Contains(t, string(dtosBody), "Reason")
}

// TestPatchDTOFile_ParseError — DTO file has syntax error.
func TestPatchDTOFile_ParseError(t *testing.T) {
	tmp := t.TempDir()
	dtosPath := filepath.Join(tmp, "broken.go")
	require.NoError(t, os.WriteFile(dtosPath, []byte("package x\nfunc {\n"), 0o644))
	chdirTest(t, tmp)
	err := patchDTOFile(FieldData{Resource: "X", DTOFile: dtosPath, WithCreate: true})
	require.Error(t, err)
}

// TestBuildModelFieldDecl_DefaultGormWhenEmpty — empty GormType triggers
// the default `gorm:"not null"` branch.
func TestBuildModelFieldDecl_DefaultGormWhenEmpty(t *testing.T) {
	got := buildModelFieldDecl(Field{Name: "X", GoType: "int", GormType: ""})
	require.Contains(t, got, `gorm:"not null"`)
}

// TestWriteFieldMigrations_MkdirError — pass a MigrationDir whose
// parent is a regular file so MkdirAll fails.
func TestWriteFieldMigrations_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "blocker"), []byte("not a dir"), 0o644))
	err := writeFieldMigrations(FieldData{
		MigrationDir: filepath.Join(tmp, "blocker", "subdir"),
		MigrationVer: "000001",
		Field:        Field{SnakeName: "x", SQLType: "VARCHAR(255)"},
		PluralSnake:  "orders",
	})
	require.Error(t, err)
}

// TestReadDBDriverSafe_NoConfig — no config.yaml present, returns "postgres".
func TestReadDBDriverSafe_NoConfig(t *testing.T) {
	chdirTest(t, t.TempDir())
	require.Equal(t, "postgres", readDBDriverSafe())
}

// TestReadDBDriverSafe_QuotedDriver — config.yaml has quoted driver value.
func TestReadDBDriverSafe_QuotedDriver(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"),
		[]byte("driver: \"mysql\"\n"), 0o644))
	chdirTest(t, tmp)
	require.Equal(t, "mysql", readDBDriverSafe())
}

// TestPatchDTOFile_MissingVariantContinues — DTO file has one of the
// variants missing; the FindStruct error is caught and continue fires.
func TestPatchDTOFile_MissingVariantContinues(t *testing.T) {
	tmp := t.TempDir()
	dtosPath := filepath.Join(tmp, "x.dtos.go")
	require.NoError(t, os.WriteFile(dtosPath,
		[]byte("package dtos\ntype OrderCreateRequest struct{}\n"), 0o644))
	chdirTest(t, tmp)
	// Request all three variants but only Create exists; Update/Response
	// missing exercise the continue branch.
	require.NoError(t, patchDTOFile(FieldData{
		Resource: "Order", DTOFile: dtosPath,
		Field:        ParseFields([]string{"reason:string"})[0],
		WithCreate:   true,
		WithUpdate:   true,
		WithResponse: true,
	}))
}

// TestPatchDTOFile_VariantAlreadyHasField — already-present field
// exercises the `continue` branch (line 132-133).
func TestPatchDTOFile_VariantAlreadyHasField(t *testing.T) {
	tmp := t.TempDir()
	dtosPath := filepath.Join(tmp, "x.dtos.go")
	require.NoError(t, os.WriteFile(dtosPath, []byte(`package dtos
type OrderCreateRequest struct{ Reason string }
`), 0o644))
	chdirTest(t, tmp)
	require.NoError(t, patchDTOFile(FieldData{
		Resource: "Order", DTOFile: dtosPath,
		Field:      ParseFields([]string{"reason:string"})[0],
		WithCreate: true,
	}))
}

// TestGenField_ModelWriteBackError — make the model file readonly so
// writeBackOrRecord's os.WriteFile fails (line 75-77).
func TestGenField_ModelWriteBackError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	modelPath := filepath.Join(tmp, "app", "models", "order.model.go")
	require.NoError(t, os.Chmod(modelPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(modelPath, 0o644) })
	err := GenField(FieldData{
		Resource: "Order",
		Field:    ParseFields([]string{"new_col:string"})[0],
	})
	require.Error(t, err)
}

// TestGenField_DTOPatchPropagatesError — DTO file is unparseable.
// GenField's WithDTO branch surfaces the patchDTOFile error.
func TestGenField_DTOPatchPropagatesError(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	dtos := filepath.Join(tmp, "app", "dtos")
	require.NoError(t, os.MkdirAll(dtos, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dtos, "order.dtos.go"),
		[]byte("package dtos\nfunc {\n"), 0o644))
	err := GenField(FieldData{
		Resource: "Order",
		Field:    ParseFields([]string{"new_col:string"})[0],
		WithDTO:  true,
	})
	require.Error(t, err)
}

// TestPatchDTOFile_WriteBackError — DTO patch succeeds in-memory but
// chmod makes write fail (line 148-150).
func TestPatchDTOFile_WriteBackError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := t.TempDir()
	dtosPath := filepath.Join(tmp, "x.dtos.go")
	require.NoError(t, os.WriteFile(dtosPath,
		[]byte("package dtos\ntype OrderCreateRequest struct{}\n"), 0o644))
	require.NoError(t, os.Chmod(dtosPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(dtosPath, 0o644) })
	chdirTest(t, t.TempDir())
	err := patchDTOFile(FieldData{
		Resource: "Order", DTOFile: dtosPath,
		Field:      ParseFields([]string{"reason:string"})[0],
		WithCreate: true,
	})
	require.Error(t, err)
}

// TestWriteFieldMigrations_WriteOrRecordCreateError — first migration
// write fails (line 188-190) because the dir is read-only after MkdirAll.
func TestWriteFieldMigrations_WriteOrRecordCreateError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := t.TempDir()
	migDir := filepath.Join(tmp, "migs")
	require.NoError(t, os.MkdirAll(migDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(migDir, 0o755) })
	err := writeFieldMigrations(FieldData{
		MigrationDir: migDir,
		MigrationVer: "000001",
		Field:        Field{SnakeName: "x", SQLType: "VARCHAR(255)"},
		PluralSnake:  "orders",
	})
	require.Error(t, err)
}

// TestGenField_AppendStructFieldError — Field.GoType containing a `}`
// makes the synthetic wrapped source unparseable, so AppendStructField
// errors (line 65-67).
func TestGenField_AppendStructFieldError(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	err := GenField(FieldData{
		Resource: "Order",
		Field: Field{
			Name:      "Bad",
			SnakeName: "bad",
			GoType:    "int }`bad",
			SQLType:   "INT",
		},
	})
	require.Error(t, err)
}

// TestPatchDTOFile_AppendStructFieldError — same trick in patchDTOFile
// path (line 138-140).
func TestPatchDTOFile_AppendStructFieldError(t *testing.T) {
	tmp := t.TempDir()
	dtosPath := filepath.Join(tmp, "x.dtos.go")
	require.NoError(t, os.WriteFile(dtosPath,
		[]byte("package dtos\ntype OrderCreateRequest struct{}\n"), 0o644))
	chdirTest(t, tmp)
	err := patchDTOFile(FieldData{
		Resource: "Order", DTOFile: dtosPath,
		Field: Field{
			Name:     "Bad",
			GoType:   "int }`bad",
			JSONName: "bad",
		},
		WithCreate: true,
	})
	require.Error(t, err)
}

// TestPatchDTOFile_TimeImportAdded — Field with time.Time triggers
// hasTimeType branch (line 144-146) in patchDTOFile.
func TestPatchDTOFile_TimeImportAdded(t *testing.T) {
	tmp := t.TempDir()
	dtosPath := filepath.Join(tmp, "x.dtos.go")
	require.NoError(t, os.WriteFile(dtosPath,
		[]byte("package dtos\ntype OrderCreateRequest struct{}\n"), 0o644))
	chdirTest(t, tmp)
	require.NoError(t, patchDTOFile(FieldData{
		Resource: "Order", DTOFile: dtosPath,
		Field:      ParseFields([]string{"shipped_at:time"})[0],
		WithCreate: true,
	}))
	body, _ := os.ReadFile(dtosPath)
	require.Contains(t, string(body), `"time"`)
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
