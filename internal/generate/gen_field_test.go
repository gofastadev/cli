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

// Real-shape fixtures mirroring the anchors templates/*.go emit for
// resource Order. Referenced types are deliberately undefined —
// astpatch parses, it never type-checks. Single quotes stand in for
// backticks (Go raw strings can't contain them); fixtureSrc converts.
const realDTOFixtureQ = `package dtos

type Order struct {
	ID    string 'json:"id"'
	Title string 'json:"title"'
}

func OrderFromModel(m *Order) *Order {
	out := &Order{
		ID:    m.ID,
		Title: m.Title,
	}
	return out
}

type TCreateOrderDto struct {
	Title *string 'json:"title" validate:"required"'
}

func (d TCreateOrderDto) ToCreateInput() CreateOrderInput {
	return CreateOrderInput{
		Title: *d.Title,
	}
}

type TUpdateOrderDto struct {
	Title *string 'json:"title,omitempty"'
}

func (d TUpdateOrderDto) ToPatch() UpdateOrderPatch {
	return UpdateOrderPatch{
		Title: d.Title,
	}
}

type TUpdateOrderGraphQLInput struct {
	Title *string 'json:"title,omitempty"'
}

func (d TUpdateOrderGraphQLInput) ToPatch() UpdateOrderPatch {
	return UpdateOrderPatch{
		Title: d.Title,
	}
}

type TOrderFiltersQueryParamsDto struct {
	Title *string 'json:"title,omitempty" schema:"title"'
}

func (q TOrderFiltersQueryParamsDto) ToFilter() ListOrdersFilter {
	f := ListOrdersFilter{
		Title: q.Title,
	}
	return f
}
`

const realInputsFixture = `package services

type CreateOrderInput struct {
	Title string
}

type UpdateOrderPatch struct {
	Title *string
}

func (p UpdateOrderPatch) AsMap() map[string]any {
	out := map[string]any{}
	if p.Title != nil {
		out["title"] = *p.Title
	}
	return out
}

type ListOrdersFilter struct {
	Title *string
}

func (f ListOrdersFilter) AsRepoFilter() map[string]any {
	out := map[string]any{}
	if f.Title != nil {
		out["title"] = *f.Title
	}
	return out
}
`

const realSvcFixture = `package services

var orderSortColumns = []string{
	"id",
	"created_at",
	"title",
}
`

const realRepoFixture = `package repositories

var orderFilterColumns = []string{
	"title",
	"is_active",
}
`

const realRepoTestFixture = `package repositories_test

func makeOrder(t T, db D) *Order {
	e := &Order{
		Title: "sample-title",
	}
	return e
}
`

const realSDLFixture = `type Order {
  id: ID!
  title: String!
}

input TCreateOrderDto {
  title: String
}

input TUpdateOrderGraphQLInput {
  title: String
}

input TOrderFiltersQueryParamsDto {
  title: String
}
`

func fixtureSrc(q string) string { return strings.ReplaceAll(q, "'", "`") }

// setupFullOrderSurfaces lays every g-field surface down at its layered
// path so GenField exercises the complete anchor set.
func setupFullOrderSurfaces(t *testing.T, tmp string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(tmp, "app", "dtos", "order.dtos.go"), fixtureSrc(realDTOFixtureQ))
	mustWriteFile(t, filepath.Join(tmp, "app", "services", "order_inputs.go"), realInputsFixture)
	mustWriteFile(t, filepath.Join(tmp, "app", "services", "order.service.go"), realSvcFixture)
	mustWriteFile(t, filepath.Join(tmp, "app", "repositories", "order.repository.go"), realRepoFixture)
	mustWriteFile(t, filepath.Join(tmp, "app", "repositories", "order.repository_test.go"), realRepoTestFixture)
	mustWriteFile(t, filepath.Join(tmp, "app", "graphql", "schema", "order.gql"), realSDLFixture)
}

// TestGenField_PatchesEverySurface — the field lands on every anchor the
// scaffold wires a field through: model, all five DTO shapes + their
// mappers, both service inputs + guarded map stmts, the sort/filter
// allowlists, the repo-test fixture, and all four SDL blocks. Running
// GenField a second time must be a byte-identical no-op (idempotency).
func TestGenField_PatchesEverySurface(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	setupFullOrderSurfaces(t, tmp)

	data := FieldData{
		Resource:     "Order",
		Field:        ParseFields([]string{"reason:string"})[0],
		WithDTO:      true,
		WithCreate:   true,
		WithUpdate:   true,
		WithResponse: true,
	}
	require.NoError(t, GenField(data))

	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(tmp, rel))
		require.NoError(t, err)
		return string(b)
	}

	dto := read("app/dtos/order.dtos.go")
	require.Contains(t, dto, "Reason string `json:\"reason\"`", "response DTO field")
	require.Contains(t, dto, "Reason: m.Reason", "FromModel mapping")
	require.Contains(t, dto, "Reason *string `json:\"reason\" validate:\"required\"`", "pointer create field")
	require.Contains(t, dto, "Reason: *d.Reason", "ToCreateInput deref")
	require.Contains(t, dto, "schema:\"reason\"", "filters schema tag")
	require.Contains(t, dto, "Reason: q.Reason", "ToFilter mapping")

	inputs := read("app/services/order_inputs.go")
	require.Contains(t, inputs, "Reason string", "Create input field")
	require.Contains(t, inputs, "Reason *string", "Update patch field")
	require.Contains(t, inputs, "out[\"reason\"] = *p.Reason", "AsMap guarded stmt")
	require.Contains(t, inputs, "out[\"reason\"] = *f.Reason", "AsRepoFilter guarded stmt")

	require.Contains(t, read("app/services/order.service.go"), "\"reason\"", "sort allowlist")
	require.Contains(t, read("app/repositories/order.repository.go"), "\"reason\"", "filter allowlist")
	require.Contains(t, read("app/repositories/order.repository_test.go"), "Reason: \"sample-reason\"", "test fixture sample")

	sdl := read("app/graphql/schema/order.gql")
	require.Contains(t, sdl, "  reason: String!", "response SDL non-null")
	require.Equal(t, 3, strings.Count(sdl, "  reason: String\n"), "create/update/filters SDL nullable")

	// Idempotency: a second run must not duplicate anything.
	before := map[string]string{}
	for _, rel := range []string{
		"app/dtos/order.dtos.go", "app/services/order_inputs.go",
		"app/services/order.service.go", "app/repositories/order.repository.go",
		"app/repositories/order.repository_test.go", "app/graphql/schema/order.gql",
	} {
		before[rel] = read(rel)
	}
	err := GenField(data)
	require.Error(t, err, "model already has the field — CodeFieldAlreadyExists guards re-runs")
	for rel, b := range before {
		require.Equal(t, b, read(rel), "%s must be untouched on the guarded second run", rel)
	}
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

// TestGenField_ScopedFlags — disabling create/update leaves those
// shapes untouched while response + filters still get the field.
func TestGenField_ScopedFlags(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	setupFullOrderSurfaces(t, tmp)

	require.NoError(t, GenField(FieldData{
		Resource:     "Order",
		Field:        ParseFields([]string{"reason:string"})[0],
		WithDTO:      true,
		WithCreate:   false,
		WithUpdate:   false,
		WithResponse: true,
	}))
	dto, err := os.ReadFile(filepath.Join(tmp, "app", "dtos", "order.dtos.go"))
	require.NoError(t, err)
	require.Contains(t, string(dto), "Reason: m.Reason", "response mapper patched")
	require.Contains(t, string(dto), "Reason: q.Reason", "filters mapper always patched")
	require.NotContains(t, string(dto), "Reason: *d.Reason", "create mapper must stay untouched")
	require.NotContains(t, string(dto), "Reason: d.Reason", "update mappers must stay untouched")
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

// TestPatchDTOFile_MissingAnchorIsHardError — a DTO file that exists
// without the scaffold anchors is a drifted file; silently skipping is
// how fields used to vanish between the model and the API surface.
func TestPatchDTOFile_MissingAnchorIsHardError(t *testing.T) {
	tmp := t.TempDir()
	dtosPath := filepath.Join(tmp, "x.dtos.go")
	require.NoError(t, os.WriteFile(dtosPath,
		[]byte("package dtos\ntype SomethingElse struct{}\n"), 0o644))
	chdirTest(t, tmp)
	err := patchDTOFile(FieldData{
		Resource: "Order", DTOFile: dtosPath,
		Field:        ParseFields([]string{"reason:string"})[0],
		WithResponse: true,
	})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodePatcherFailed), ce.Code)
	require.Contains(t, err.Error(), "drifted")
}

// TestPatchDTOFile_FieldAlreadyEverywhereIsNoOp — a field already
// present on every anchor patches nothing and errors nothing.
func TestPatchDTOFile_FieldAlreadyEverywhereIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	setupFullOrderSurfaces(t, tmp)
	dtosPath := filepath.Join(tmp, "app", "dtos", "order.dtos.go")
	before, err := os.ReadFile(dtosPath)
	require.NoError(t, err)

	require.NoError(t, patchDTOFile(FieldData{
		Resource: "Order", DTOFile: dtosPath,
		Field:        ParseFields([]string{"title:string"})[0],
		WithCreate:   true,
		WithUpdate:   true,
		WithResponse: true,
	}))
	after, err := os.ReadFile(dtosPath)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
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
	chdirTest(t, tmp)
	setupFullOrderSurfaces(t, tmp)
	dtosPath := filepath.Join(tmp, "app", "dtos", "order.dtos.go")
	require.NoError(t, os.Chmod(dtosPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(dtosPath, 0o644) })
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
	chdirTest(t, tmp)
	setupFullOrderSurfaces(t, tmp)
	err := patchDTOFile(FieldData{
		Resource: "Order",
		DTOFile:  filepath.Join(tmp, "app", "dtos", "order.dtos.go"),
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
	chdirTest(t, tmp)
	setupFullOrderSurfaces(t, tmp)
	dtosPath := filepath.Join(tmp, "app", "dtos", "order.dtos.go")
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

// TestFieldAlterAddSQL_DriverForms — T-SQL has no COLUMN keyword, and
// NOT NULL columns gain a zero-value DEFAULT so the migration applies
// to populated tables.
func TestFieldAlterAddSQL_DriverForms(t *testing.T) {
	f := Field{SnakeName: "sku", GoType: "string", SQLType: "VARCHAR(255) NOT NULL"}

	pg := fieldAlterAddSQL("postgres", "orders", f)
	require.Equal(t, "ALTER TABLE orders ADD COLUMN sku VARCHAR(255) NOT NULL DEFAULT '';\n", pg)

	ms := fieldAlterAddSQL("sqlserver", "orders", Field{SnakeName: "qty", GoType: "int", SQLType: "INT NOT NULL"})
	require.Equal(t, "ALTER TABLE orders ADD qty INT NOT NULL DEFAULT 0;\n", ms)

	// A type that already carries a DEFAULT is left untouched.
	b := fieldAlterAddSQL("postgres", "orders", Field{SnakeName: "ok", GoType: "bool", SQLType: "BOOLEAN NOT NULL DEFAULT false"})
	require.Equal(t, "ALTER TABLE orders ADD COLUMN ok BOOLEAN NOT NULL DEFAULT false;\n", b)

	u := fieldAlterAddSQL("mysql", "orders", Field{SnakeName: "owner_id", GoType: "uuid.UUID", SQLType: "CHAR(36) NOT NULL"})
	require.Contains(t, u, "DEFAULT '00000000-0000-0000-0000-000000000000'")
}

// TestGenField_DryRunCreatesNoDirectories — the MkdirAll that used to
// run before the planner chokepoint materialized db/migrations/ even
// in preview mode.
func TestGenField_DryRunCreatesNoDirectories(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	require.NoError(t, os.RemoveAll(filepath.Join(tmp, "db")))
	resetPlannerState(t)
	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	require.NoError(t, GenField(FieldData{Resource: "Order", Field: Field{
		Name: "Notes", JSONName: "notes", SnakeName: "notes", GoType: "string",
		GormType: `gorm:"not null"`, SQLType: "VARCHAR(255) NOT NULL",
	}}))

	_, err := os.Stat(filepath.Join(tmp, "db", "migrations"))
	require.True(t, os.IsNotExist(err), "dry-run must not create db/migrations/")
}

// TestPatchSDLFile_MissingBlockIsHardError — a schema file that exists
// without the scaffold's block headers is drift, not a skip.
func TestPatchSDLFile_MissingBlockIsHardError(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	schema := filepath.Join(tmp, "order.gql")
	require.NoError(t, os.WriteFile(schema, []byte("type SomethingElse {\n  id: ID!\n}\n"), 0o644))

	err := patchSDLFile(FieldData{
		Resource: "Order", SchemaFile: schema,
		Field:        ParseFields([]string{"reason:string"})[0],
		WithResponse: true,
	})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodePatcherFailed), ce.Code)
}

// TestPatchAllowlistVar_MissingVarIsHardError — an existing service
// file without the allowlist var is drift.
func TestPatchAllowlistVar_MissingVarIsHardError(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	svc := filepath.Join(tmp, "order.service.go")
	require.NoError(t, os.WriteFile(svc, []byte("package services\n"), 0o644))

	err := patchAllowlistVar(svc, "orderSortColumns", "reason")
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodePatcherFailed), ce.Code)
}

// TestPatchRepoTestFixture_MissingBuilderIsSkip — repo test files are
// user-owned after generation; no make<R> builder just means nothing
// to extend, not an error.
func TestPatchRepoTestFixture_MissingBuilderIsSkip(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	rt := filepath.Join(tmp, "order.repository_test.go")
	require.NoError(t, os.WriteFile(rt, []byte("package repositories_test\n"), 0o644))

	require.NoError(t, patchRepoTestFixture(FieldData{
		Resource: "Order", RepoTestFile: rt,
		Field: ParseFields([]string{"reason:string"})[0],
	}))
}
