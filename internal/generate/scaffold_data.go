package generate

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/layout"
)

// identifierPattern is the strict allow-list for user-supplied resource
// and field names. A valid name starts with a letter and then contains
// only letters, digits, underscores, and hyphens. The hyphen and
// underscore are included because the generators already accept
// kebab_case / snake_case input (toPascalCase splits on `-` and `_`,
// e.g. the `send-email` task); everything else — `/`, `..`, whitespace,
// quotes, semicolons, `{` — is rejected. Those names would otherwise
// flow unescaped into file paths (app/models/<snake>.model.go,
// db/migrations/…) and into rendered SQL/Go templates, making them a
// path-traversal / template-injection vector. A leading `.` (so `..`
// and `../x`) fails the `[A-Za-z]` anchor, so traversal is blocked even
// though `-`/`_` are allowed after the first character.
var identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// validateIdentifier rejects a resource or field name that is not a
// plain identifier. Returns clierr.CodeInvalidName — the same code the
// sibling generators (validateEndpoint / validateRelation /
// validateRename) use for the same class of error.
func validateIdentifier(name string) error {
	if !identifierPattern.MatchString(name) {
		return clierr.Newf(clierr.CodeInvalidName,
			"invalid name %q: must start with a letter and contain only letters, digits, underscores, and hyphens", name)
	}
	return nil
}

// BuildScaffoldData converts a resource name and fields into fully computed ScaffoldData.
func BuildScaffoldData(name string, fields []Field) ScaffoldData {
	pascal := toPascalCase(name)
	plural := pluralize(pascal)
	driver := configutil.ReadDBDriver()

	// Resolve per-driver SQL type into the active SQLType field
	for i := range fields {
		fields[i].SQLType = resolveSQLType(fields[i], driver)
	}

	return ScaffoldData{
		Name:         pascal,
		LowerName:    toCamelCase(name),
		SnakeName:    toSnakeCase(name),
		PluralName:   plural,
		PluralSnake:  toSnakeCase(plural),
		PluralLower:  toCamelCase(plural),
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
