// gen_rename.go — `gofasta g rename <Resource>.<OldField> <NewField> [--apply]`
//
// Renames a single field across every place gofasta's layered scaffold
// touches it:
//
//   - app/models/<snake>.model.go      — struct field + GORM column tag
//   - app/dtos/<snake>.dtos.go         — every DTO that uses the field
//   - app/services/<snake>.service.go  — receiver-method field references
//   - db/migrations/NNNNNN_rename_<old>_to_<new>_on_<plural>.up.sql/.down.sql
//
// Conservative by design — runs in *preview* mode by default so the user
// can confirm the diff before any disk write. Pass --apply to commit the
// changes. This keeps the rename safe: cross-file string surgery is
// inherently risky and the user should see exactly what's about to land.
//
// The substitution is *token-aware* (regex with word boundaries) so we
// don't accidentally rewrite "Total" inside "TotalCount". The match key
// is the PascalCase struct-field form; underlying GORM tags and JSON
// names are rewritten via dedicated rules.
package generate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
)

// RenameData is the resolved input.
type RenameData struct {
	Resource string // PascalCase ("Order")
	OldField string // PascalCase ("Total")
	NewField string // PascalCase ("AmountCents")
	Apply    bool   // false = preview only (default); true = write to disk
}

// GenRename is the entry point invoked by the Cobra command.
func GenRename(d RenameData) error {
	d = renameDataDefaults(d)
	if err := validateRename(d); err != nil {
		return err
	}

	// Compute the file set + substitution rules.
	targets := renameTargets(d.Resource)
	subs := renameSubstitutions(d.OldField, d.NewField)

	// Step 1: read every target, compute patched bodies, decide whether
	// anything actually changed. Dry-run records, --apply writes.
	for _, path := range targets {
		body, err := os.ReadFile(path)
		if err != nil {
			continue // missing layer is fine — model-only resources are valid
		}
		patched := applyRenameRules(body, subs)
		if bytes.Equal(patched, body) {
			continue
		}
		detail := fmt.Sprintf("rename %s.%s → %s", d.Resource, d.OldField, d.NewField)
		// Preview mode = act as if dry-run regardless of GetDryRun().
		if !d.Apply {
			recordPatch(path, detail, len(patched))
			continue
		}
		if err := os.WriteFile(path, patched, 0o644); err != nil {
			return clierr.Wrap(clierr.CodeFileIO, err, "writing "+path)
		}
	}

	// Step 2: write the rename migration.
	plural := toSnakeCase(pluralize(d.Resource))
	col := toSnakeCase(d.OldField)
	newCol := toSnakeCase(d.NewField)
	ver := nextMigrationNumber()

	up := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;\n", plural, col, newCol)
	down := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;\n", plural, newCol, col)

	upPath := filepath.Join("db", "migrations",
		fmt.Sprintf("%s_rename_%s_to_%s_on_%s.up.sql", ver, col, newCol, plural))
	downPath := filepath.Join("db", "migrations",
		fmt.Sprintf("%s_rename_%s_to_%s_on_%s.down.sql", ver, col, newCol, plural))

	// In preview mode, record the planned creates; in --apply mode, write.
	if !d.Apply {
		recordCreate(upPath, len(up))
		recordCreate(downPath, len(down))
		return nil
	}
	if err := writeOrRecordCreate(upPath, []byte(up)); err != nil {
		return err
	}
	return writeOrRecordCreate(downPath, []byte(down))
}

func renameDataDefaults(d RenameData) RenameData {
	d.Resource = toPascalCase(d.Resource)
	d.OldField = toPascalCase(d.OldField)
	d.NewField = toPascalCase(d.NewField)
	return d
}

func validateRename(d RenameData) error {
	if d.Resource == "" || d.OldField == "" || d.NewField == "" {
		return clierr.New(clierr.CodeInvalidName,
			"`g rename <Resource>.<OldField> <NewField>` — all three required")
	}
	if d.OldField == d.NewField {
		return clierr.New(clierr.CodeInvalidName, "old and new field names are identical")
	}
	return nil
}

// renameTargets returns the file paths a rename can touch for a given
// resource. Missing files are skipped at apply time so model-only
// resources work fine.
func renameTargets(resource string) []string {
	snake := toSnakeCase(resource)
	return []string{
		filepath.Join("app", "models", snake+".model.go"),
		filepath.Join("app", "dtos", snake+".dtos.go"),
		filepath.Join("app", "services", snake+".service.go"),
		filepath.Join("app", "services", snake+".service_test.go"),
		filepath.Join("app", "repositories", snake+".repository.go"),
	}
}

// renameSubst describes one substitution rule (regex → replacement).
type renameSubst struct {
	pattern     *regexp.Regexp
	replacement string
}

// renameSubstitutions builds the substitution rule set for a single
// rename. The rules are token-aware: we match \bOldField\b on the Go
// side and the corresponding snake_case / camelCase / json-tag forms on
// the tag side.
func renameSubstitutions(oldField, newField string) []renameSubst {
	oldPascal := oldField
	newPascal := newField
	oldSnake := toSnakeCase(oldField)
	newSnake := toSnakeCase(newField)
	oldCamel := toCamelCase(oldField)
	newCamel := toCamelCase(newField)

	return []renameSubst{
		{regexp.MustCompile(`\b` + regexp.QuoteMeta(oldPascal) + `\b`), newPascal},
		{regexp.MustCompile(`column:` + regexp.QuoteMeta(oldSnake) + `\b`), "column:" + newSnake},
		{regexp.MustCompile(`json:"` + regexp.QuoteMeta(oldCamel) + `"`), "json:\"" + newCamel + "\""},
		{regexp.MustCompile(`json:"` + regexp.QuoteMeta(oldSnake) + `"`), "json:\"" + newSnake + "\""},
	}
}

// applyRenameRules runs every substitution against body in order. The
// matches are independent so the order doesn't matter for correctness;
// we run them sequentially for determinism.
func applyRenameRules(body []byte, rules []renameSubst) []byte {
	out := body
	for _, r := range rules {
		out = r.pattern.ReplaceAll(out, []byte(r.replacement))
	}
	return out
}

// pluralize and toSnakeCase / toCamelCase live in scaffold_data.go's
// neighbors. Adding small wrappers here would shadow them, so we leave
// the call sites to use the package-level helpers directly.

// toCamelCaseSafe is unused — placeholder to avoid an import-needed
// rebuild when the package's other helpers haven't yet been wired in.
var _ = strings.ToLower
