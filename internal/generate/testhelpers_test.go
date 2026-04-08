package generate

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTempProject creates a temp dir with a minimal go.mod and db/migrations dir,
// changes cwd to it, and returns a cleanup function that restores the original cwd.
func setupTempProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	os.MkdirAll(filepath.Join(dir, "db", "migrations"), 0755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/testorg/testapp\n\ngo 1.25.8\n"), 0644)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("database:\n  driver: postgres\n"), 0644)

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

// sampleScaffoldData returns a fully populated ScaffoldData for testing.
func sampleScaffoldData() ScaffoldData {
	return ScaffoldData{
		Name:              "Product",
		LowerName:         "product",
		SnakeName:         "product",
		PluralName:        "Products",
		PluralSnake:       "products",
		PluralLower:       "products",
		Fields:            sampleFields(),
		MigrationNum:      "000001",
		IncludeController: true,
		IncludeGraphQL:    false,
		DBDriver:          "postgres",
		ModulePath:        "github.com/testorg/testapp",
	}
}

// sampleFields returns a set of fields for testing.
func sampleFields() []Field {
	return []Field{
		{
			Name:            "Name",
			JSONName:        "name",
			SnakeName:       "name",
			GoType:          "string",
			GormType:        `gorm:"not null"`,
			GQLType:         "String",
			SQLType:         "VARCHAR(255) NOT NULL",
			SQLTypePostgres: "VARCHAR(255) NOT NULL",
			SQLTypeMySQL:    "VARCHAR(255) NOT NULL",
			SQLTypeSQLite:   "TEXT NOT NULL",
		},
		{
			Name:            "Price",
			JSONName:        "price",
			SnakeName:       "price",
			GoType:          "float64",
			GormType:        `gorm:"not null"`,
			GQLType:         "Float",
			SQLType:         "DECIMAL(10,2) NOT NULL",
			SQLTypePostgres: "DECIMAL(10,2) NOT NULL",
			SQLTypeMySQL:    "DECIMAL(10,2) NOT NULL",
			SQLTypeSQLite:   "REAL NOT NULL",
		},
	}
}

// writeTestFile is a helper to write a file in the current temp directory.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// readTestFile reads a file and returns its content.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
