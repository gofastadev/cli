package generate

import (
	"os"
	"testing"

	"github.com/gofastadev/cli/internal/generate/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationTemplates_Postgres(t *testing.T) {
	up, down := migrationTemplates("postgres")
	assert.Equal(t, templates.MigUpPostgres, up)
	assert.Equal(t, templates.MigDownPostgres, down)
}

func TestMigrationTemplates_MySQL(t *testing.T) {
	up, down := migrationTemplates("mysql")
	assert.Equal(t, templates.MigUpMySQL, up)
	assert.Equal(t, templates.MigDownMySQL, down)
}

func TestMigrationTemplates_SQLite(t *testing.T) {
	up, down := migrationTemplates("sqlite")
	assert.Equal(t, templates.MigUpSQLite, up)
	assert.Equal(t, templates.MigDownSQLite, down)
}

func TestMigrationTemplates_SQLServer(t *testing.T) {
	up, down := migrationTemplates("sqlserver")
	assert.Equal(t, templates.MigUpSQLServer, up)
	assert.Equal(t, templates.MigDownSQLServer, down)
}

func TestMigrationTemplates_ClickHouse(t *testing.T) {
	up, down := migrationTemplates("clickhouse")
	assert.Equal(t, templates.MigUpClickHouse, up)
	assert.Equal(t, templates.MigDownClickHouse, down)
}

func TestMigrationTemplates_DefaultIsPostgres(t *testing.T) {
	up, down := migrationTemplates("oracle")
	assert.Equal(t, templates.MigUpPostgres, up)
	assert.Equal(t, templates.MigDownPostgres, down)
}

func TestGenMigration_CreatesFiles(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenMigration(d)
	require.NoError(t, err)

	upPath := "db/migrations/000001_create_products.up.sql"
	downPath := "db/migrations/000001_create_products.down.sql"

	_, err = os.Stat(upPath)
	assert.NoError(t, err, "up migration should exist")

	_, err = os.Stat(downPath)
	assert.NoError(t, err, "down migration should exist")

	upContent := readTestFile(t, upPath)
	assert.Contains(t, upContent, "products")
}

func TestGenMigration_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	writeTestFile(t, "db/migrations/000001_create_products.up.sql", "original up")
	writeTestFile(t, "db/migrations/000001_create_products.down.sql", "original down")

	err := GenMigration(d)
	require.NoError(t, err)

	assert.Equal(t, "original up", readTestFile(t, "db/migrations/000001_create_products.up.sql"))
	assert.Equal(t, "original down", readTestFile(t, "db/migrations/000001_create_products.down.sql"))
}
