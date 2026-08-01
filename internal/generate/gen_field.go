// gen_field.go — `gofasta g field <Resource> <name>:<type>`
//
// Adds a single field across the four places that always need to move
// together for a "just one more column" change:
//
//  1. db/migrations/NNNNNN_add_<field>_to_<plural>.up.sql / .down.sql
//  2. app/models/<snake>.model.go    (field on the model struct + GORM tag)
//  3. (optional) app/dtos/<snake>.dtos.go   (Request + Update + Response)
//
// Each downstream surface is opt-out via flags so the user can add a
// model-only column (--no-dto), a response-only field (--no-update
// --no-create), etc.
package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/generate/astpatch"
	"github.com/gofastadev/cli/internal/layout"
	"github.com/gofastadev/cli/internal/naming"
)

// FieldData is the resolved input for the field generator.
type FieldData struct {
	Resource           string // PascalCase ("Order")
	LowerName          string // camelCase ("order", "apiKey") — allowlist var prefix
	Snake              string // snake_case ("order")
	PluralSnake        string // snake_case plural ("orders")
	PluralName         string // PascalCase plural ("Orders") — List<Plural>Filter
	Field              Field  // parsed name:type with GORM + SQL tags resolved
	WithDTO            bool   // include DTO/inputs/allowlist/SDL patches (default true)
	WithCreate         bool   // include TCreate<R>Dto + Create<R>Input (default true)
	WithUpdate         bool   // include TUpdate<R>Dto/GraphQLInput + Update<R>Patch (default true)
	WithResponse       bool   // include the <R> response DTO + FromModel (default true)
	ModelFile          string
	DTOFile            string
	InputsFile         string
	SvcFile            string
	RepoFile           string
	RepoTestFile       string
	ControllerTestFile string
	SchemaFile         string // app/graphql/schema/<snake>.gql (empty file = no GraphQL)
	MigrationDir       string
	MigrationVer       string // 6-digit version prefix; computed by default
	DBDriver           string
}

// GenField is the entry point invoked by the Cobra command.
func GenField(d FieldData) error {
	d = fieldDataDefaults(d)

	if err := ensureExists(d.ModelFile); err != nil {
		return err
	}

	// Step 1: patch the model struct.
	mf, err := astpatch.Parse(d.ModelFile)
	if err != nil {
		return err
	}
	st, err := astpatch.FindStruct(mf, d.Resource)
	if err != nil {
		return err
	}
	if astpatch.StructHasField(st, d.Field.Name) {
		return clierr.Newf(clierr.CodeFieldAlreadyExists,
			"model %s already has field %s — pick a different name",
			d.Resource, d.Field.Name)
	}
	fieldDecl := buildModelFieldDecl(d.Field)
	if err := astpatch.AppendStructField(st, fieldDecl); err != nil {
		return err
	}
	if hasTimeType(d.Field) {
		astpatch.EnsureImport(mf, "time")
	}
	if d.Field.GoType == "uuid.UUID" {
		astpatch.EnsureImport(mf, "github.com/google/uuid")
	}
	if err := writeBackOrRecord(mf,
		fmt.Sprintf("add %s field to model %s", d.Field.Name, d.Resource)); err != nil {
		return err
	}

	// Step 2: patch every downstream surface the scaffold wires a field
	// through (opt-out via --no-dto).
	if d.WithDTO {
		if err := patchFieldSurfaces(d); err != nil {
			return err
		}
	}

	// Step 3: write the migration pair.
	return writeFieldMigrations(d)
}

// patchFieldSurfaces runs the field patch across the DTO, inputs,
// allowlist, test-fixture, and SDL surfaces. Files that don't exist
// are skipped — a `g model`-only resource legitimately has no DTOs —
// but a file that EXISTS with a missing anchor is a hard error:
// silently "patching" nothing is how fields used to vanish between
// the model and the API surface.
func patchFieldSurfaces(d FieldData) error {
	steps := []struct {
		path  string
		patch func() error
	}{
		{d.DTOFile, func() error { return patchDTOFile(d) }},
		{d.InputsFile, func() error { return patchInputsFile(d) }},
		{d.SvcFile, func() error {
			return patchAllowlistVar(d.SvcFile, d.LowerName+"SortColumns", d.Field.SnakeName)
		}},
		{d.RepoFile, func() error {
			return patchAllowlistVar(d.RepoFile, d.LowerName+"FilterColumns", d.Field.SnakeName)
		}},
		{d.RepoTestFile, func() error { return patchRepoTestFixture(d) }},
		{d.ControllerTestFile, func() error { return patchControllerTestBody(d) }},
		{d.SchemaFile, func() error { return patchSDLFile(d) }},
	}
	for _, s := range steps {
		if !fileExistsHelper(s.path) {
			continue
		}
		if err := s.patch(); err != nil {
			return err
		}
	}
	return nil
}

func fieldDataDefaults(d FieldData) FieldData {
	if d.Resource != "" && d.Snake == "" {
		d.Snake = toSnakeCase(d.Resource)
	}
	if d.Resource != "" && d.LowerName == "" {
		d.LowerName = naming.Camel(d.Resource)
	}
	if d.Resource != "" && d.PluralName == "" {
		d.PluralName = pluralize(toPascalCase(d.Resource))
	}
	if d.PluralSnake == "" && d.Resource != "" {
		d.PluralSnake = toSnakeCase(d.PluralName)
	}
	d = fieldPathDefaults(d)
	if d.MigrationVer == "" {
		d.MigrationVer = nextMigrationNumber()
	}
	if d.DBDriver == "" {
		d.DBDriver = readDBDriverSafe()
	}
	// Resolve per-driver SQL type if not already set.
	if d.Field.SQLType == "" {
		d.Field.SQLType = resolveSQLType(d.Field, d.DBDriver)
	}
	return d
}

// anchorErr wraps an astpatch miss as a hard PATCHER_FAILED error. A
// file that exists with a missing anchor means it drifted from the
// scaffold shape — silently skipping used to make the field vanish
// between the model and the API surface.
func anchorErr(err error, file, anchor string) error {
	return clierr.Wrapf(clierr.CodePatcherFailed, err,
		"%s exists but anchor %q was not found — the file has drifted from the scaffold shape; restore the anchor or pass --no-dto",
		file, anchor)
}

// patchStructField appends fieldSrc to struct name in f, skipping when
// the field already exists. A missing struct is a hard error.
func patchStructField(f *astpatch.File, name, fieldSrc, fieldName string) (bool, error) {
	st, err := astpatch.FindStruct(f, name)
	if err != nil {
		return false, anchorErr(err, f.Path, "struct "+name)
	}
	if astpatch.StructHasField(st, fieldName) {
		return false, nil
	}
	if err := astpatch.AppendStructField(st, fieldSrc); err != nil {
		return false, err
	}
	return true, nil
}

// patchMapperComposite appends `key: <expr>,` to the single struct
// literal built inside func recv.fnName. A missing func is a hard error.
func patchMapperComposite(f *astpatch.File, recv, fnName, key, exprSrc string) (bool, error) {
	fn, err := astpatch.FindFunc(f, recv, fnName)
	if err != nil {
		return false, anchorErr(err, f.Path, "func "+recv+"."+fnName)
	}
	lit, err := astpatch.FirstCompositeLitInFunc(fn)
	if err != nil {
		return false, anchorErr(err, f.Path, "composite literal in "+fnName)
	}
	if astpatch.CompositeLitHasKey(lit, key) {
		return false, nil
	}
	if err := astpatch.AppendKeyValueToCompositeLit(lit, key, exprSrc); err != nil {
		return false, err
	}
	return true, nil
}

// fieldPathDefaults resolves every unset file path from the detected
// layout. Split out of fieldDataDefaults purely to keep each function
// within the complexity budget.
func fieldPathDefaults(d FieldData) FieldData {
	lo := layout.Detect()
	defaults := []struct {
		dst *string
		val string
	}{
		{&d.ModelFile, lo.ModelFile(d.Snake)},
		{&d.DTOFile, lo.DTOsFile(d.Snake)},
		{&d.InputsFile, lo.InputsFile(d.Snake)},
		{&d.SvcFile, lo.SvcImplFile(d.Snake)},
		{&d.RepoFile, lo.RepoImplFile(d.Snake)},
		{&d.RepoTestFile, lo.RepoTestFile(d.Snake)},
		{&d.ControllerTestFile, lo.ControllerTestFile(d.Snake)},
		{&d.SchemaFile, filepath.Join("app", "graphql", "schema", d.Snake+".gql")},
		{&d.MigrationDir, lo.MigrationsDir()},
	}
	for _, def := range defaults {
		if *def.dst == "" {
			*def.dst = def.val
		}
	}
	return d
}

// patchDTOFile threads the field through every wire shape in
// app/dtos/<snake>.dtos.go: the response DTO + FromModel, the pointer
// Create DTO + ToCreateInput deref, both Update shapes + their ToPatch
// mappers, and the filters DTO + ToFilter. Anchors match the
// generator's templates (templates/dtos.go) exactly.
func patchDTOFile(d FieldData) error {
	df, err := astpatch.Parse(d.DTOFile)
	if err != nil {
		return err
	}
	r, fld := d.Resource, d.Field
	patched := false
	type structPatch struct {
		enabled  bool
		name     string
		fieldSrc string
	}
	type mapperPatch struct {
		enabled bool
		recv    string
		fn      string
		expr    string
	}
	structPatches := []structPatch{
		{d.WithResponse, r,
			fmt.Sprintf("%s %s `json:%q`", fld.Name, fld.GoType, fld.JSONName)},
		{d.WithCreate, "TCreate" + r + "Dto",
			fmt.Sprintf("%s *%s `json:%q validate:\"required\"`", fld.Name, fld.GoType, fld.JSONName)},
		{d.WithUpdate, "TUpdate" + r + "Dto",
			fmt.Sprintf("%s *%s `json:\"%s,omitempty\"`", fld.Name, fld.GoType, fld.JSONName)},
		{d.WithUpdate, "TUpdate" + r + "GraphQLInput",
			fmt.Sprintf("%s *%s `json:\"%s,omitempty\"`", fld.Name, fld.GoType, fld.JSONName)},
		{true, "T" + r + "FiltersQueryParamsDto",
			fmt.Sprintf("%s *%s `json:\"%s,omitempty\" schema:%q`", fld.Name, fld.GoType, fld.JSONName, fld.JSONName)},
	}
	mapperPatches := []mapperPatch{
		{d.WithResponse, "", r + "FromModel", "m." + fld.Name},
		{d.WithCreate, "TCreate" + r + "Dto", "ToCreateInput", "*d." + fld.Name},
		{d.WithUpdate, "TUpdate" + r + "Dto", "ToPatch", "d." + fld.Name},
		{d.WithUpdate, "TUpdate" + r + "GraphQLInput", "ToPatch", "d." + fld.Name},
		{true, "T" + r + "FiltersQueryParamsDto", "ToFilter", "q." + fld.Name},
	}
	for _, p := range structPatches {
		if !p.enabled {
			continue
		}
		did, err := patchStructField(df, p.name, p.fieldSrc, fld.Name)
		if err != nil {
			return err
		}
		patched = patched || did
	}
	for _, p := range mapperPatches {
		if !p.enabled {
			continue
		}
		did, err := patchMapperComposite(df, p.recv, p.fn, fld.Name, p.expr)
		if err != nil {
			return err
		}
		patched = patched || did
	}
	if !patched {
		return nil
	}
	ensureFieldTypeImports(df, fld)
	return writeBackOrRecord(df,
		fmt.Sprintf("add %s field to DTOs of %s", fld.Name, d.Resource))
}

// patchInputsFile threads the field through the service-layer shapes in
// app/services/<snake>_inputs.go: Create<R>Input, Update<R>Patch +
// AsMap, List<Plural>Filter + AsRepoFilter.
func patchInputsFile(d FieldData) error {
	f, err := astpatch.Parse(d.InputsFile)
	if err != nil {
		return err
	}
	r, fld := d.Resource, d.Field
	patched := false
	if d.WithCreate {
		did, err := patchStructField(f, "Create"+r+"Input",
			fmt.Sprintf("%s %s", fld.Name, fld.GoType), fld.Name)
		if err != nil {
			return err
		}
		patched = patched || did
	}
	if d.WithUpdate {
		did, err := patchStructField(f, "Update"+r+"Patch",
			fmt.Sprintf("%s *%s", fld.Name, fld.GoType), fld.Name)
		if err != nil {
			return err
		}
		patched = patched || did
		did, err = patchGuardedMapStmt(f, "Update"+r+"Patch", "AsMap", "p", fld)
		if err != nil {
			return err
		}
		patched = patched || did
	}
	did, err := patchStructField(f, "List"+d.PluralName+"Filter",
		fmt.Sprintf("%s *%s", fld.Name, fld.GoType), fld.Name)
	if err != nil {
		return err
	}
	patched = patched || did
	did, err = patchGuardedMapStmt(f, "List"+d.PluralName+"Filter", "AsRepoFilter", "f", fld)
	if err != nil {
		return err
	}
	patched = patched || did
	if !patched {
		return nil
	}
	ensureFieldTypeImports(f, fld)
	return writeBackOrRecord(f,
		fmt.Sprintf("add %s field to service inputs of %s", fld.Name, d.Resource))
}

// patchGuardedMapStmt inserts the nil-guarded map assignment that AsMap
// and AsRepoFilter use for every optional field:
//
//	if <recvVar>.<Field> != nil {
//		out["<snake>"] = *<recvVar>.<Field>
//	}
//
// before the function's return. Idempotency keys on the column-name
// string literal already appearing in the body.
func patchGuardedMapStmt(f *astpatch.File, recv, fnName, recvVar string, fld Field) (bool, error) {
	fn, err := astpatch.FindFunc(f, recv, fnName)
	if err != nil {
		return false, anchorErr(err, f.Path, "func "+recv+"."+fnName)
	}
	if astpatch.FuncContainsStringLit(fn, fld.SnakeName) {
		return false, nil
	}
	stmt := fmt.Sprintf("if %s.%s != nil {\n\tout[%q] = *%s.%s\n}",
		recvVar, fld.Name, fld.SnakeName, recvVar, fld.Name)
	if err := astpatch.InsertStmtBeforeReturn(fn, stmt); err != nil {
		return false, err
	}
	return true, nil
}

// patchAllowlistVar appends the column to a `var <name> = []string{...}`
// allowlist (the service's sortColumns / the repo's FilterColumns) —
// without this, a freshly added field would silently be unsortable and
// unfilterable.
func patchAllowlistVar(path, varName, column string) error {
	f, err := astpatch.Parse(path)
	if err != nil {
		return err
	}
	lit, err := astpatch.FindVarCompositeLit(f, varName)
	if err != nil {
		return anchorErr(err, path, "var "+varName)
	}
	if astpatch.CompositeLitHasString(lit, column) {
		return nil
	}
	astpatch.AppendStringToCompositeLit(lit, column)
	return writeBackOrRecord(f, fmt.Sprintf("add %q to %s", column, varName))
}

// patchRepoTestFixture extends the generated make<R> test fixture so
// the repository tests keep exercising real column values.
func patchRepoTestFixture(d FieldData) error {
	f, err := astpatch.Parse(d.RepoTestFile)
	if err != nil {
		return err
	}
	fn, err := astpatch.FindFunc(f, "", "make"+d.Resource)
	if err != nil {
		// Repo test files are user-owned after generation; a missing
		// fixture builder just means nothing to extend.
		return nil
	}
	lit, err := astpatch.FirstCompositeLitInFunc(fn)
	if err != nil {
		return nil
	}
	if astpatch.CompositeLitHasKey(lit, d.Field.Name) {
		return nil
	}
	if err := astpatch.AppendKeyValueToCompositeLit(lit, d.Field.Name, d.Field.SampleLiteral()); err != nil {
		return err
	}
	ensureFieldTypeImports(f, d.Field)
	return writeBackOrRecord(f,
		fmt.Sprintf("add %s to make%s test fixture", d.Field.Name, d.Resource))
}

// patchControllerTestBody extends the JSON request body in the
// generated Test<R>Controller_Create_ServiceError_500 test. That test
// posts a fully-populated body past a noop validator straight into
// ToCreateInput, whose pointer derefs assume every required field is
// present — a field added after scaffold time would nil-panic the test.
// Same skip policy as the repo-test fixture: test files are user-owned,
// so a missing test or reshaped body means nothing generated is left to
// keep in sync.
func patchControllerTestBody(d FieldData) error {
	if !d.WithCreate {
		return nil
	}
	f, err := astpatch.Parse(d.ControllerTestFile)
	if err != nil {
		return err
	}
	fn, err := astpatch.FindFunc(f, "", "Test"+d.Resource+"Controller_Create_ServiceError_500")
	if err != nil {
		return nil
	}
	lit := findJSONBodyLit(fn)
	if lit == nil {
		return nil
	}
	if strings.Contains(lit.Value, "\""+d.Field.JSONName+"\"") {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(lit.Value, "`"), "`")
	entry := fmt.Sprintf("%q: %s", d.Field.JSONName, d.Field.SampleJSON())
	closing := strings.LastIndex(inner, "}")
	if closing == -1 {
		return nil
	}
	sep := ", "
	if !strings.Contains(inner, ":") {
		sep = "" // `{ }` body — first field, no leading comma
	}
	lit.Value = "`" + inner[:closing] + sep + entry + " " + inner[closing:] + "`"
	return writeBackOrRecord(f,
		fmt.Sprintf("add %s to the Create test body of %s", d.Field.JSONName, d.Resource))
}

// findJSONBodyLit returns the raw-string literal holding the test's
// JSON request body (the only backtick literal starting with `{` in
// the function).
func findJSONBodyLit(fn *dst.FuncDecl) *dst.BasicLit {
	var found *dst.BasicLit
	dst.Inspect(fn.Body, func(n dst.Node) bool {
		if found != nil {
			return false
		}
		if bl, ok := n.(*dst.BasicLit); ok && strings.HasPrefix(bl.Value, "`{") {
			found = bl
			return false
		}
		return true
	})
	return found
}

// ensureFieldTypeImports adds the imports a field's Go type needs.
func ensureFieldTypeImports(f *astpatch.File, fld Field) {
	if hasTimeType(fld) {
		astpatch.EnsureImport(f, "time")
	}
	if fld.GoType == "uuid.UUID" {
		astpatch.EnsureImport(f, "github.com/google/uuid")
	}
}

// patchSDLFile inserts the field into the GraphQL schema fragment's
// four per-resource blocks. Text-based (SDL has no Go AST): each block
// is located by its exact header line from templates/graphql.go, and
// the field line goes immediately before the block's closing brace.
// Create/Update/Filters inputs stay nullable — the bound Go DTOs use
// pointer fields (present-vs-absent semantics); the validator enforces
// required on create.
func patchSDLFile(d FieldData) error {
	raw, err := osReadFileFn(d.SchemaFile)
	if err != nil {
		return clierr.Wrapf(clierr.CodeFileIO, err, "reading %s", d.SchemaFile)
	}
	content := string(raw)
	fld := d.Field
	type block struct {
		enabled bool
		header  string
		line    string
	}
	blocks := []block{
		{d.WithResponse, "type " + d.Resource + " {",
			fmt.Sprintf("  %s: %s!", fld.JSONName, fld.GQLType)},
		{d.WithCreate, "input TCreate" + d.Resource + "Dto {",
			fmt.Sprintf("  %s: %s", fld.JSONName, fld.GQLType)},
		{d.WithUpdate, "input TUpdate" + d.Resource + "GraphQLInput {",
			fmt.Sprintf("  %s: %s", fld.JSONName, fld.GQLType)},
		{true, "input T" + d.Resource + "FiltersQueryParamsDto {",
			fmt.Sprintf("  %s: %s", fld.JSONName, fld.GQLType)},
	}
	patched := false
	for _, b := range blocks {
		if !b.enabled {
			continue
		}
		updated, did, err := insertSDLField(content, b.header, b.line, fld.JSONName, d.SchemaFile)
		if err != nil {
			return err
		}
		content = updated
		patched = patched || did
	}
	if !patched {
		return nil
	}
	if err := writeOrRecordPatch(d.SchemaFile,
		fmt.Sprintf("add %s to GraphQL schema of %s", fld.JSONName, d.Resource),
		[]byte(content)); err != nil {
		return err
	}
	cliout.Hint("GraphQL schema updated — run `go tool gqlgen generate` (or `gofasta gqlgen`) to regenerate resolvers")
	return nil
}

// insertSDLField adds line before the closing `}` of the block opened
// by header. Idempotent on a `<json>:` field already present inside
// the block. A missing block header is a hard error (the schema file
// exists, so it must carry the scaffold's blocks).
func insertSDLField(content, header, line, jsonName, path string) (updated string, patched bool, err error) {
	start := strings.Index(content, header)
	if start == -1 {
		return content, false, clierr.Newf(clierr.CodePatcherFailed,
			"%s exists but block %q was not found — the schema has drifted from the scaffold shape; restore the block or pass --no-dto",
			path, strings.TrimSuffix(header, " {"))
	}
	bodyStart := start + len(header)
	end := strings.Index(content[bodyStart:], "\n}")
	if end == -1 {
		return content, false, clierr.Newf(clierr.CodePatcherFailed,
			"%s: block %q has no closing brace", path, header)
	}
	blockBody := content[bodyStart : bodyStart+end]
	for _, l := range strings.Split(blockBody, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), jsonName+":") {
			return content, false, nil
		}
	}
	insertAt := bodyStart + end
	return content[:insertAt] + "\n" + line + content[insertAt:], true, nil
}

// writeFieldMigrations emits the .up.sql and .down.sql files for adding
// (and dropping) the column. Directory creation happens inside the
// writeOrRecordCreate chokepoint so `--dry-run` leaves the filesystem
// untouched (a bare MkdirAll here used to materialize db/migrations/
// even in preview mode).
func writeFieldMigrations(d FieldData) error {
	upName := fmt.Sprintf("%s_add_%s_to_%s.up.sql",
		d.MigrationVer, d.Field.SnakeName, d.PluralSnake)
	downName := fmt.Sprintf("%s_add_%s_to_%s.down.sql",
		d.MigrationVer, d.Field.SnakeName, d.PluralSnake)

	upBody := fieldAlterAddSQL(d.DBDriver, d.PluralSnake, d.Field)
	downBody := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;\n",
		d.PluralSnake, d.Field.SnakeName)

	if err := writeOrRecordCreate(filepath.Join(d.MigrationDir, upName), []byte(upBody)); err != nil {
		return err
	}
	return writeOrRecordCreate(filepath.Join(d.MigrationDir, downName), []byte(downBody))
}

// fieldAlterAddSQL builds the driver-correct ADD-column statement.
// Two portability fixes over the previous one-liner:
//
//   - T-SQL has no COLUMN keyword in ALTER TABLE ... ADD — the previous
//     form failed on every SQL Server project.
//   - A NOT NULL column without a DEFAULT cannot be added to a table
//     that already has rows (Postgres 23502, SQLite "Cannot add a NOT
//     NULL column..."). Scalar types gain a zero-value DEFAULT; bool
//     and time types already carry one from the type table.
func fieldAlterAddSQL(driver, table string, f Field) string {
	sqlType := f.SQLType
	if strings.Contains(sqlType, "NOT NULL") && !strings.Contains(sqlType, "DEFAULT") {
		if def := sqlZeroDefault(f.GoType); def != "" {
			sqlType += " DEFAULT " + def
		}
	}
	if driver == "sqlserver" {
		return fmt.Sprintf("ALTER TABLE %s ADD %s %s;\n", table, f.SnakeName, sqlType)
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;\n", table, f.SnakeName, sqlType)
}

// sqlZeroDefault maps a Go field type to the SQL literal used as the
// zero-value DEFAULT for newly added NOT NULL columns.
func sqlZeroDefault(goType string) string {
	switch goType {
	case "string":
		return "''"
	case "int", "float64":
		return "0"
	case "uuid.UUID":
		return "'00000000-0000-0000-0000-000000000000'"
	default:
		return ""
	}
}

// buildModelFieldDecl renders one model struct field line including
// the GORM tag emitted by fieldparse's per-driver resolution.
func buildModelFieldDecl(f Field) string {
	gorm := f.GormType
	if gorm == "" {
		gorm = `gorm:"not null"`
	}
	return fmt.Sprintf("%s %s `%s`", f.Name, f.GoType, gorm)
}

func hasTimeType(f Field) bool { return f.GoType == "time.Time" }

// fileExistsHelper is a local mirror of os.Stat-based existence check —
// kept inline to avoid pulling fileExists from the commands package.
func fileExistsHelper(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readDBDriverSafe never errors — gofasta-managed projects always have a
// config.yaml, but a malformed file shouldn't kill the generator. Fall
// back to postgres.
func readDBDriverSafe() string {
	defer func() { _ = recover() }()
	if data, err := os.ReadFile("config.yaml"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "driver:") {
				continue
			}
			val := strings.TrimSpace(strings.TrimPrefix(line, "driver:"))
			val = strings.Trim(val, `"'`)
			if val != "" {
				return val
			}
		}
	}
	return "postgres"
}
