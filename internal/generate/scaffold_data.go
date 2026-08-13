package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/layout"
	"github.com/gofastadev/cli/internal/naming"
)

// validateIdentifier rejects a field / job / task / template name that
// is not a plain identifier (letters, digits, underscores, hyphens;
// starts with a letter). Names outside this set would flow unescaped
// into file paths (app/models/<snake>.model.go, db/migrations/…) and
// into rendered SQL/Go templates, making them a path-traversal /
// template-injection vector. A leading `.` (so `..` and `../x`) fails
// the leading-letter anchor, so traversal is blocked even though `-`
// and `_` are allowed after the first character. Returns
// clierr.CodeInvalidName — the same code the sibling validators
// (validateEndpoint / validateRelation / validateRename) use.
func validateIdentifier(name string) error {
	if !naming.IsIdentifier(name) {
		return clierr.Newf(clierr.CodeInvalidName,
			"invalid name %q: must start with a letter and contain only letters, digits, underscores, and hyphens", name)
	}
	return nil
}

// validateResourceName is validateIdentifier's strict sibling for
// RESOURCE names: hyphens are additionally rejected because resources
// become Go package directories and feature-layout import aliases
// (`<snake>pkg`), and `blog-postpkg` is not a valid Go identifier.
// Jobs, tasks, and email templates keep their kebab-case freedom via
// validateIdentifier.
func validateResourceName(name string) error {
	if !naming.IsResourceName(name) {
		return clierr.Newf(clierr.CodeInvalidName,
			"invalid resource name %q: must start with a letter and contain only letters, digits, and underscores (hyphens aren't allowed — resources become Go package names)", name)
	}
	return nil
}

// BuildScaffoldData converts a resource name and fields into fully computed ScaffoldData.
func BuildScaffoldData(name string, fields []Field) ScaffoldData {
	pascal := naming.Pascal(name)
	plural := naming.Pluralize(pascal)
	driver := configutil.ReadDBDriver()

	// Resolve per-driver SQL type into the active SQLType field
	for i := range fields {
		fields[i].SQLType = resolveSQLType(fields[i], driver)
	}

	return ScaffoldData{
		Name: pascal,
		// LowerName is a Go identifier prefix (apiKeySortColumns), so it
		// uses the golint camel form — leading initialism fully lowered.
		LowerName:    naming.Camel(name),
		SnakeName:    naming.Snake(name),
		PluralName:   plural,
		PluralSnake:  naming.Snake(plural),
		PluralLower:  naming.Camel(plural),
		Fields:       fields,
		MigrationNum: nextMigrationNumber(),
		DBDriver:     driver,
		ModulePath:   readModulePath(),
		Layout:       layout.Detect(),
	}
}

func nextMigrationNumber() string {
	entries, _ := os.ReadDir("db/migrations")
	highest := 0
	for _, e := range entries {
		if len(e.Name()) >= 6 {
			var num int
			_, _ = fmt.Sscanf(e.Name()[:6], "%d", &num)
			if num > highest {
				highest = num
			}
		}
	}
	return fmt.Sprintf("%06d", highest+1)
}

// readModulePath reads the module path from go.mod in the current directory.
func readModulePath() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// resolveSQLType picks the correct SQL type string for the active database driver.
func resolveSQLType(f Field, driver string) string {
	switch driver {
	case "mysql":
		return f.SQLTypeMySQL
	case "sqlite":
		return f.SQLTypeSQLite
	case "sqlserver":
		return f.SQLTypeSQLServer
	case "clickhouse":
		return f.SQLTypeClickHouse
	default:
		return f.SQLTypePostgres
	}
}
