package templates

import (
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testScaffoldData struct {
	Name              string
	LowerName         string
	SnakeName         string
	PluralName        string
	PluralSnake       string
	PluralLower       string
	Fields            []testField
	MigrationNum      string
	IncludeController bool
	IncludeGraphQL    bool
	IncludeSwagger    bool
	DBDriver          string
	ModulePath        string
}

type testField struct {
	Name      string
	JSONName  string
	SnakeName string
	GoType    string
	GormType  string
	GQLType   string
	SQLType   string
}

func sampleData() testScaffoldData {
	return testScaffoldData{
		Name:         "Product",
		LowerName:    "product",
		SnakeName:    "product",
		PluralName:   "Products",
		PluralSnake:  "products",
		PluralLower:  "products",
		MigrationNum: "000001",
		DBDriver:     "postgres",
		ModulePath:   "github.com/testorg/testapp",
		Fields: []testField{
			{Name: "Name", JSONName: "name", SnakeName: "name", GoType: "string", GormType: `gorm:"not null"`, GQLType: "String", SQLType: "VARCHAR(255) NOT NULL"},
			{Name: "Price", JSONName: "price", SnakeName: "price", GoType: "float64", GormType: `gorm:"not null"`, GQLType: "Float", SQLType: "DECIMAL(10,2) NOT NULL"},
		},
	}
}

var funcMap = template.FuncMap{
	"timestamp": func() string { return time.Now().Format(time.RFC3339) },
	"lbrace":    func() string { return "{" },
	"rbrace":    func() string { return "}" },
}

func TestAllTemplatesAreParseable(t *testing.T) {
	templates := map[string]string{
		"Model":         Model,
		"Controller":    Controller,
		"DTOs":          DTOs,
		"Repo":          Repo,
		"RepoInterface": RepoInterface,
		"Svc":           Svc,
		"SvcInterface":  SvcInterface,
		"Routes":        Routes,
		"WireProvider":  WireProvider,
		"GraphQL":       GraphQL,
	}
	for name, tmpl := range templates {
		t.Run(name, func(t *testing.T) {
			_, err := template.New(name).Funcs(funcMap).Parse(tmpl)
			require.NoError(t, err)
		})
	}
}

func TestAllTemplatesRenderWithSampleData(t *testing.T) {
	data := sampleData()
	templates := map[string]string{
		"Model":         Model,
		"Controller":    Controller,
		"DTOs":          DTOs,
		"Repo":          Repo,
		"RepoInterface": RepoInterface,
		"Svc":           Svc,
		"SvcInterface":  SvcInterface,
		"Routes":        Routes,
		"WireProvider":  WireProvider,
		"GraphQL":       GraphQL,
	}
	for name, tmpl := range templates {
		t.Run(name, func(t *testing.T) {
			parsed, err := template.New(name).Funcs(funcMap).Parse(tmpl)
			require.NoError(t, err)
			var buf strings.Builder
			err = parsed.Execute(&buf, data)
			require.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestModelTemplate_Content(t *testing.T) {
	data := sampleData()
	parsed, err := template.New("model").Funcs(funcMap).Parse(Model)
	require.NoError(t, err)
	var buf strings.Builder
	require.NoError(t, parsed.Execute(&buf, data))
	output := buf.String()
	assert.Contains(t, output, "package models")
	assert.Contains(t, output, "Product")
	assert.Contains(t, output, "Name")
	assert.Contains(t, output, "Price")
}

func TestMigrationTemplates_AllDrivers(t *testing.T) {
	data := sampleData()
	migrationPairs := map[string][2]string{
		"postgres":   {MigUpPostgres, MigDownPostgres},
		"mysql":      {MigUpMySQL, MigDownMySQL},
		"sqlite":     {MigUpSQLite, MigDownSQLite},
		"sqlserver":  {MigUpSQLServer, MigDownSQLServer},
		"clickhouse": {MigUpClickHouse, MigDownClickHouse},
	}
	for driver, pair := range migrationPairs {
		t.Run(driver+"_up", func(t *testing.T) {
			parsed, err := template.New("up").Funcs(funcMap).Parse(pair[0])
			require.NoError(t, err)
			var buf strings.Builder
			require.NoError(t, parsed.Execute(&buf, data))
			output := buf.String()
			assert.NotEmpty(t, output)
			assert.Contains(t, output, "products")
		})
		t.Run(driver+"_down", func(t *testing.T) {
			parsed, err := template.New("down").Funcs(funcMap).Parse(pair[1])
			require.NoError(t, err)
			var buf strings.Builder
			require.NoError(t, parsed.Execute(&buf, data))
			output := buf.String()
			assert.NotEmpty(t, output)
		})
	}
}
