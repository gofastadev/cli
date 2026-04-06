package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenMigration(d ScaffoldData) error {
	up := fmt.Sprintf("db/migrations/%s_create_%s.up.sql", d.MigrationNum, d.PluralSnake)
	down := fmt.Sprintf("db/migrations/%s_create_%s.down.sql", d.MigrationNum, d.PluralSnake)

	upTpl, downTpl := migrationTemplates(d.DBDriver)

	if err := WriteTemplate(up, "mig_up", upTpl, d); err != nil {
		return err
	}
	return WriteTemplate(down, "mig_down", downTpl, d)
}

func migrationTemplates(driver string) (up, down string) {
	switch driver {
	case "mysql":
		return templates.MigUpMySQL, templates.MigDownMySQL
	case "sqlite":
		return templates.MigUpSQLite, templates.MigDownSQLite
	case "sqlserver":
		return templates.MigUpSQLServer, templates.MigDownSQLServer
	case "clickhouse":
		return templates.MigUpClickHouse, templates.MigDownClickHouse
	default:
		return templates.MigUpPostgres, templates.MigDownPostgres
	}
}
