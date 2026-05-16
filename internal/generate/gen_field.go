// gen_field.go — `gofasta g field <Resource> <name>:<type>`
//
// Adds a single field across the four places that always need to move
// together for a "just one more column" change:
//
//   1. db/migrations/NNNNNN_add_<field>_to_<plural>.up.sql / .down.sql
//   2. app/models/<snake>.model.go    (field on the model struct + GORM tag)
//   3. (optional) app/dtos/<snake>.dtos.go   (Request + Update + Response)
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

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
)

// FieldData is the resolved input for the field generator.
type FieldData struct {
	Resource     string // PascalCase ("Order")
	Snake        string // snake_case ("order")
	PluralSnake  string // snake_case plural ("orders")
	Field        Field  // parsed name:type with GORM + SQL tags resolved
	WithDTO      bool   // include DTO patches (default true)
	WithCreate   bool   // include CreateRequest field (default true)
	WithUpdate   bool   // include UpdateRequest field (default true)
	WithResponse bool   // include Response field (default true)
	ModelFile    string
	DTOFile      string
	MigrationDir string
	MigrationVer string // 6-digit version prefix; computed by default
	DBDriver     string
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
	if _, err := writeBackOrRecord(mf,
		fmt.Sprintf("add %s field to model %s", d.Field.Name, d.Resource)); err != nil {
		return err
	}

	// Step 2: patch the DTOs (optional).
	if d.WithDTO && fileExistsHelper(d.DTOFile) {
		if err := patchDTOFile(d); err != nil {
			return err
		}
	}

	// Step 3: write the migration pair.
	if err := writeFieldMigrations(d); err != nil {
		return err
	}
	return nil
}

func fieldDataDefaults(d FieldData) FieldData {
	if d.Resource != "" && d.Snake == "" {
		d.Snake = toSnakeCase(d.Resource)
	}
	if d.PluralSnake == "" && d.Resource != "" {
		d.PluralSnake = toSnakeCase(pluralize(toPascalCase(d.Resource)))
	}
	if d.ModelFile == "" {
		d.ModelFile = filepath.Join("app", "models", d.Snake+".model.go")
	}
	if d.DTOFile == "" {
		d.DTOFile = filepath.Join("app", "dtos", d.Snake+".dtos.go")
	}
	if d.MigrationDir == "" {
		d.MigrationDir = filepath.Join("db", "migrations")
	}
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

// patchDTOFile appends the new field to whichever DTOs the user opted
// into via --with-create / --with-update / --with-response.
func patchDTOFile(d FieldData) error {
	df, err := astpatch.Parse(d.DTOFile)
	if err != nil {
		return err
	}
	patched := false
	for _, variant := range dtoVariants(d) {
		st, err := astpatch.FindStruct(df, variant)
		if err != nil {
			continue // missing variant is non-fatal — user may not have defined it
		}
		if astpatch.StructHasField(st, d.Field.Name) {
			continue
		}
		// DTOs don't carry GORM tags; emit a plain field with a json tag.
		field := fmt.Sprintf("%s %s `json:%q`",
			d.Field.Name, d.Field.GoType, d.Field.JSONName)
		if err := astpatch.AppendStructField(st, field); err != nil {
			return err
		}
		patched = true
	}
	if patched {
		if hasTimeType(d.Field) {
			astpatch.EnsureImport(df, "time")
		}
		if _, err := writeBackOrRecord(df,
			fmt.Sprintf("add %s field to DTOs of %s", d.Field.Name, d.Resource)); err != nil {
			return err
		}
	}
	return nil
}

// dtoVariants returns the DTO type names to patch given the WithCreate /
// WithUpdate / WithResponse flags. Defaults (in field generator) are all
// true so users get the field everywhere unless they opt out.
func dtoVariants(d FieldData) []string {
	var out []string
	if d.WithCreate {
		out = append(out, d.Resource+"CreateRequest")
	}
	if d.WithUpdate {
		out = append(out, d.Resource+"UpdateRequest")
	}
	if d.WithResponse {
		out = append(out, d.Resource+"Response")
	}
	return out
}

// writeFieldMigrations emits the .up.sql and .down.sql files for adding
// (and dropping) the column.
func writeFieldMigrations(d FieldData) error {
	if err := os.MkdirAll(d.MigrationDir, 0o755); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "mkdir "+d.MigrationDir)
	}
	upName := fmt.Sprintf("%s_add_%s_to_%s.up.sql",
		d.MigrationVer, d.Field.SnakeName, d.PluralSnake)
	downName := fmt.Sprintf("%s_add_%s_to_%s.down.sql",
		d.MigrationVer, d.Field.SnakeName, d.PluralSnake)

	upBody := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;\n",
		d.PluralSnake, d.Field.SnakeName, d.Field.SQLType)
	downBody := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;\n",
		d.PluralSnake, d.Field.SnakeName)

	if err := writeOrRecordCreate(filepath.Join(d.MigrationDir, upName), []byte(upBody)); err != nil {
		return err
	}
	if err := writeOrRecordCreate(filepath.Join(d.MigrationDir, downName), []byte(downBody)); err != nil {
		return err
	}
	return nil
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
