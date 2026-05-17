// gen_relation.go — `gofasta g relation <Resource> belongs_to|has_many|has_one <Other>`
//
// Adds an association between two resources:
//
//   - belongs_to <Other>  → model gains <Other>ID + *<Other>; FK column added
//     to <Resource>'s table via migration.
//   - has_many  <Other>   → model gains []<Other>; no schema change on
//     <Resource> itself — the FK lives on <Other>'s
//     table (a sibling g relation belongs_to call
//     on <Other> handles that side).
//   - has_one   <Other>   → model gains *<Other>; FK lives on <Other>.
//
// We never assume both sides of the relation need patching — that's a
// product decision (the user may have a one-way navigation property).
// Run `g relation` twice, once per side, when bidirectional navigation
// is desired.
package generate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
)

// RelationKind enumerates the supported gorm/sql relationship shapes.
type RelationKind string

// Relation kinds the generator accepts as the second positional argument.
// Each maps to a distinct GORM tag layout + (for `belongs_to`) a paired
// FK migration on the parent table.
const (
	RelationBelongsTo RelationKind = "belongs_to"
	RelationHasMany   RelationKind = "has_many"
	RelationHasOne    RelationKind = "has_one"
)

// RelationData is the resolved input.
type RelationData struct {
	Resource string       // PascalCase parent ("Order")
	Other    string       // PascalCase related ("Customer")
	Kind     RelationKind // belongs_to | has_many | has_one

	ResourceModel string // path override
	OtherModel    string // path override
	MigrationDir  string
	MigrationVer  string
}

// GenRelation is the entry point invoked by the Cobra command.
func GenRelation(d RelationData) error {
	d = relationDataDefaults(d)
	if err := validateRelation(d); err != nil {
		return err
	}
	if err := ensureExists(d.ResourceModel); err != nil {
		return err
	}

	// Step 1: patch the resource model.
	mf, err := astpatch.Parse(d.ResourceModel)
	if err != nil {
		return err
	}
	st, err := astpatch.FindStruct(mf, d.Resource)
	if err != nil {
		return err
	}
	for _, fldDecl := range relationModelFields(d) {
		fieldName := strings.SplitN(strings.TrimSpace(fldDecl), " ", 2)[0]
		if astpatch.StructHasField(st, fieldName) {
			continue // idempotent
		}
		if err := astpatch.AppendStructField(st, fldDecl); err != nil {
			return err
		}
	}
	if d.Kind == RelationBelongsTo {
		astpatch.EnsureImport(mf, "github.com/google/uuid")
	}
	if err := writeBackOrRecord(mf,
		fmt.Sprintf("add %s %s on %s", d.Kind, d.Other, d.Resource)); err != nil {
		return err
	}

	// Step 2: emit migration when the FK lives on this resource's table.
	if d.Kind == RelationBelongsTo {
		return writeRelationMigration(d)
	}
	return nil
}

func relationDataDefaults(d RelationData) RelationData {
	if d.Resource != "" && d.ResourceModel == "" {
		d.ResourceModel = filepath.Join("app", "models", toSnakeCase(d.Resource)+".model.go")
	}
	if d.Other != "" && d.OtherModel == "" {
		d.OtherModel = filepath.Join("app", "models", toSnakeCase(d.Other)+".model.go")
	}
	if d.MigrationDir == "" {
		d.MigrationDir = filepath.Join("db", "migrations")
	}
	if d.MigrationVer == "" {
		d.MigrationVer = nextMigrationNumber()
	}
	return d
}

func validateRelation(d RelationData) error {
	if d.Resource == "" || d.Other == "" {
		return clierr.New(clierr.CodeInvalidName,
			"both <Resource> and <Other> are required")
	}
	switch d.Kind {
	case RelationBelongsTo, RelationHasMany, RelationHasOne:
		return nil
	default:
		return clierr.Newf(clierr.CodeInvalidName,
			"relation kind %q must be belongs_to | has_many | has_one", d.Kind)
	}
}

// relationModelFields returns the struct field lines this relation adds
// to the resource's model.
func relationModelFields(d RelationData) []string {
	switch d.Kind {
	case RelationBelongsTo:
		return []string{
			fmt.Sprintf(`%sID uuid.UUID %s`, d.Other,
				"`gorm:\"type:uuid;not null\"`"),
			fmt.Sprintf(`%s *%s %s`, d.Other, d.Other,
				"`gorm:\"foreignKey:"+d.Other+"ID\"`"),
		}
	case RelationHasMany:
		return []string{
			fmt.Sprintf(`%s []%s`, pluralize(d.Other), d.Other),
		}
	case RelationHasOne:
		return []string{
			fmt.Sprintf(`%s *%s`, d.Other, d.Other),
		}
	}
	return nil
}

// writeRelationMigration emits the .up.sql/.down.sql pair for adding the
// FK column on the resource's table.
func writeRelationMigration(d RelationData) error {
	parentTable := toSnakeCase(pluralize(d.Resource))
	otherTable := toSnakeCase(pluralize(d.Other))
	col := toSnakeCase(d.Other) + "_id"
	constraint := fmt.Sprintf("fk_%s_%s", parentTable, col)

	upName := fmt.Sprintf("%s_add_%s_to_%s.up.sql",
		d.MigrationVer, col, parentTable)
	downName := fmt.Sprintf("%s_add_%s_to_%s.down.sql",
		d.MigrationVer, col, parentTable)

	up := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN %s uuid NOT NULL;\n"+
			"ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (id);\n",
		parentTable, col, parentTable, constraint, col, otherTable)
	down := fmt.Sprintf(
		"ALTER TABLE %s DROP CONSTRAINT %s;\n"+
			"ALTER TABLE %s DROP COLUMN %s;\n",
		parentTable, constraint, parentTable, col)

	if err := writeOrRecordCreate(filepath.Join(d.MigrationDir, upName), []byte(up)); err != nil {
		return err
	}
	return writeOrRecordCreate(filepath.Join(d.MigrationDir, downName), []byte(down))
}
