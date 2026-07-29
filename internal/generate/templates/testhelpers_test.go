package templates

import (
	"html/template"
	"time"
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

// HasTimeField mirrors the helper on the production ScaffoldData type. The
// model template uses it to gate the `import "time"` line — without this
// method the template would always render and the test data would fail
// the template engine's strict field check.
func (s testScaffoldData) HasTimeField() bool {
	for _, f := range s.Fields {
		if f.GoType == "time.Time" {
			return true
		}
	}
	return false
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
