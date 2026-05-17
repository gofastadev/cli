package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
