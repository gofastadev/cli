package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSQLType_Postgres(t *testing.T) {
	f := Field{SQLTypePostgres: "VARCHAR(255) NOT NULL", SQLTypeMySQL: "VARCHAR(255) NOT NULL"}
	assert.Equal(t, "VARCHAR(255) NOT NULL", resolveSQLType(f, "postgres"))
}

func TestResolveSQLType_MySQL(t *testing.T) {
	f := Field{SQLTypePostgres: "VARCHAR(255)", SQLTypeMySQL: "VARCHAR(255) NOT NULL"}
	assert.Equal(t, "VARCHAR(255) NOT NULL", resolveSQLType(f, "mysql"))
}

func TestResolveSQLType_SQLite(t *testing.T) {
	f := Field{SQLTypeSQLite: "TEXT NOT NULL"}
	assert.Equal(t, "TEXT NOT NULL", resolveSQLType(f, "sqlite"))
}

func TestResolveSQLType_SQLServer(t *testing.T) {
	f := Field{SQLTypeSQLServer: "NVARCHAR(255) NOT NULL"}
	assert.Equal(t, "NVARCHAR(255) NOT NULL", resolveSQLType(f, "sqlserver"))
}

func TestResolveSQLType_ClickHouse(t *testing.T) {
	f := Field{SQLTypeClickHouse: "String"}
	assert.Equal(t, "String", resolveSQLType(f, "clickhouse"))
}

func TestResolveSQLType_UnknownDefaultsToPostgres(t *testing.T) {
	f := Field{SQLTypePostgres: "INTEGER NOT NULL"}
	assert.Equal(t, "INTEGER NOT NULL", resolveSQLType(f, "oracle"))
}

func TestNextMigrationNumber_EmptyDir(t *testing.T) {
	setupTempProject(t)
	assert.Equal(t, "000001", nextMigrationNumber())
}

func TestNextMigrationNumber_ExistingFiles(t *testing.T) {
	setupTempProject(t)
	writeTestFile(t, "db/migrations/000001_create_users.up.sql", "")
	writeTestFile(t, "db/migrations/000001_create_users.down.sql", "")
	writeTestFile(t, "db/migrations/000003_create_products.up.sql", "")
	writeTestFile(t, "db/migrations/000003_create_products.down.sql", "")
	assert.Equal(t, "000004", nextMigrationNumber())
}

func TestNextMigrationNumber_NoDirExists(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)
	// No db/migrations dir
	assert.Equal(t, "000001", nextMigrationNumber())
}

func TestReadModulePath(t *testing.T) {
	setupTempProject(t)
	assert.Equal(t, "github.com/testorg/testapp", readModulePath())
}

func TestReadModulePath_NoModuleLine(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.25.8\n"), 0644)
	os.Chdir(dir)
	assert.Equal(t, "", readModulePath())
}

func TestReadModulePath_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)
	assert.Equal(t, "", readModulePath())
}

func TestBuildScaffoldData(t *testing.T) {
	setupTempProject(t)
	// Write a config.yaml with postgres driver
	os.WriteFile("config.yaml", []byte("database:\n  driver: postgres\n"), 0644)

	fields := ParseFields([]string{"name:string", "price:float"})
	d := BuildScaffoldData("product", fields)

	assert.Equal(t, "Product", d.Name)
	assert.Equal(t, "product", d.LowerName)
	assert.Equal(t, "product", d.SnakeName)
	assert.Equal(t, "Products", d.PluralName)
	assert.Equal(t, "products", d.PluralSnake)
	assert.Equal(t, "products", d.PluralLower)
	assert.Equal(t, "000001", d.MigrationNum)
	assert.Equal(t, "postgres", d.DBDriver)
	assert.Equal(t, "github.com/testorg/testapp", d.ModulePath)
	require.Len(t, d.Fields, 2)
	// Verify SQLType was resolved to postgres
	assert.Equal(t, "VARCHAR(255) NOT NULL", d.Fields[0].SQLType)
	assert.Equal(t, "DECIMAL(10,2) NOT NULL", d.Fields[1].SQLType)
}

func TestBuildScaffoldData_MySQL(t *testing.T) {
	setupTempProject(t)
	os.WriteFile("config.yaml", []byte("database:\n  driver: mysql\n"), 0644)

	fields := ParseFields([]string{"name:string"})
	d := BuildScaffoldData("product", fields)

	assert.Equal(t, "mysql", d.DBDriver)
	require.Len(t, d.Fields, 1)
	assert.Equal(t, "VARCHAR(255) NOT NULL", d.Fields[0].SQLType)
}

func TestBuildScaffoldData_UnderscoreName(t *testing.T) {
	setupTempProject(t)

	d := BuildScaffoldData("order_item", ParseFields([]string{"qty:int"}))
	assert.Equal(t, "OrderItem", d.Name)
	assert.Equal(t, "orderItem", d.LowerName)
	assert.Equal(t, "order_item", d.SnakeName)
	assert.Equal(t, "OrderItems", d.PluralName)
}

func TestBuildScaffoldData_IncrementsMigrationNum(t *testing.T) {
	setupTempProject(t)
	writeTestFile(t, "db/migrations/000001_create_users.up.sql", "")

	d := BuildScaffoldData("product", nil)
	assert.Equal(t, "000002", d.MigrationNum)
}

func TestBuildScaffoldData_NoConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.MkdirAll(filepath.Join(dir, "db", "migrations"), 0755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testmod\n\ngo 1.25.8\n"), 0644)
	os.Chdir(dir)

	d := BuildScaffoldData("item", nil)
	assert.Equal(t, "postgres", d.DBDriver) // defaults to postgres
	assert.Equal(t, "testmod", d.ModulePath)
}
