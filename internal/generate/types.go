package generate

import (
	"strings"

	"github.com/gofastadev/cli/internal/layout"
)

// Field represents a single field in a resource (parsed from "name:type" CLI args).
type Field struct {
	Name      string // PascalCase: ProductName
	JSONName  string // camelCase: productName
	SnakeName string // snake_case: product_name
	GoType    string // Go type: string
	GormType  string // GORM tag: gorm:"not null"
	GQLType   string // GraphQL type: String
	SQLType   string // SQL type (generic): VARCHAR(255) NOT NULL
	// Per-driver SQL types (populated by field parser based on DBDriver)
	SQLTypePostgres   string
	SQLTypeMySQL      string
	SQLTypeSQLite     string
	SQLTypeSQLServer  string
	SQLTypeClickHouse string
}

// SampleLiteral returns a Go literal suitable as a test-fixture value
// for this field's type. Used by the generated test templates so
// fixtures exercise real column values instead of leaving a TODO.
// uuid/time literals assume the rendering template already imports
// github.com/google/uuid and time (repotest and svctest both do).
func (f Field) SampleLiteral() string {
	switch f.GoType {
	case "string":
		// `text` and `string` share GoType; the GORM tag is the only
		// marker that distinguishes them (see fieldparse.go).
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

// ScaffoldData holds all computed names and fields for template rendering.
type ScaffoldData struct {
	Name              string // PascalCase: Product
	LowerName         string // camelCase: product
	SnakeName         string // snake_case: product
	PluralName        string // PascalCase plural: Products
	PluralSnake       string // snake_case plural: products
	PluralLower       string // camelCase plural: products
	Fields            []Field
	MigrationNum      string
	IncludeController bool
	IncludeGraphQL    bool
	IncludeSwagger    bool
	Schedule          string // cron expression for job generator
	DBDriver          string // database driver from config (postgres, mysql, sqlite, sqlserver, clickhouse)
	ModulePath        string // Go module path read from go.mod (e.g., "github.com/myorg/myapp")
	Layout            layout.Layout
}

// HasTimeField reports whether any field on this resource is `time.Time`.
// Used by templates that must emit `import "time"` only when needed —
// otherwise gofmt/imports complains about an unused import.
func (s ScaffoldData) HasTimeField() bool {
	for _, f := range s.Fields {
		if f.GoType == "time.Time" {
			return true
		}
	}
	return false
}

// HasUUIDField is HasTimeField's twin for `uuid.UUID` fields — gates
// the github.com/google/uuid import in templates that don't otherwise
// import it (model, inputs).
func (s ScaffoldData) HasUUIDField() bool {
	for _, f := range s.Fields {
		if f.GoType == "uuid.UUID" {
			return true
		}
	}
	return false
}

// L returns the layout for this scaffold operation, defaulting to
// layered when ScaffoldData was constructed without one. Production
// callers go through BuildScaffoldData which always sets Layout; this
// defaulting exists so test code that constructs ScaffoldData literals
// (and any future caller that does the same) keeps working without
// every test having to set the field. The default matches the historical
// behavior — every gen_*.go used to hardcode the layered paths.
func (s ScaffoldData) L() layout.Layout {
	if s.Layout == nil {
		return layout.For(layout.Layered)
	}
	return s.Layout
}

// Step is a single unit of work in a generator pipeline.
type Step struct {
	Label string
	Fn    func(ScaffoldData) error
}
