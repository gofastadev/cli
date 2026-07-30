package templates

import (
	"html/template"
	"strings"
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
	FeatureLayout     bool
}

// testLayout mirrors the subset of layout.Layout the templates consult.
type testLayout struct{ Feature bool }

func (l testLayout) IsFeature() bool { return l.Feature }

// L mirrors ScaffoldData.L() so templates that branch on
// {{if .L.IsFeature}} render against test data.
func (s testScaffoldData) L() testLayout {
	return testLayout{Feature: s.FeatureLayout}
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

// SampleLiteral mirrors the helper on the production Field type (see
// internal/generate/types.go) so test templates that emit fixture
// values render against test data.
func (f testField) SampleLiteral() string {
	switch f.GoType {
	case "string":
		if strings.Contains(f.GormType, "type:text") {
			return `"sample text"`
		}
		return `"sample-` + f.SnakeName + `"`
	case "int":
		return "1"
	case "float64":
		return "1.5"
	case "bool":
		return "true"
	case "uuid.UUID":
		return "uuid.New()"
	case "time.Time":
		return "time.Now().UTC()"
	default:
		return `""`
	}
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
			{Name: "Summary", JSONName: "summary", SnakeName: "summary", GoType: "string", GormType: `gorm:"type:text;not null"`, GQLType: "String", SQLType: "TEXT NOT NULL"},
			{Name: "OwnerID", JSONName: "ownerId", SnakeName: "owner_id", GoType: "uuid.UUID", GormType: `gorm:"type:uuid;not null"`, GQLType: "ID", SQLType: "UUID NOT NULL"},
			{Name: "ReleasedAt", JSONName: "releasedAt", SnakeName: "released_at", GoType: "time.Time", GormType: `gorm:"type:timestamp;not null"`, GQLType: "DateTime", SQLType: "TIMESTAMP NOT NULL DEFAULT now()"},
		},
	}
}

var funcMap = template.FuncMap{
	"timestamp": func() string { return time.Now().Format(time.RFC3339) },
	"lbrace":    func() string { return "{" },
	"rbrace":    func() string { return "}" },
}
