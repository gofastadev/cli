package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/commands/configutil"
)

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
	}
}

func nextMigrationNumber() string {
	entries, _ := os.ReadDir("db/migrations")
	max := 0
	for _, e := range entries {
		if len(e.Name()) >= 6 {
			var num int
			fmt.Sscanf(e.Name()[:6], "%d", &num)
			if num > max {
				max = num
			}
		}
	}
	return fmt.Sprintf("%06d", max+1)
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
