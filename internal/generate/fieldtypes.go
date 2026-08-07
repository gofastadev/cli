package generate

import "slices"

// FieldType describes one supported "name:type" field kind: the Go/GORM/
// GraphQL representations plus the column type for every supported SQL
// driver. The registry below is the single source of truth for supported
// types — ParseFields resolves against it, and `gofasta facts` exposes it
// so documentation tables can be generated instead of hand-maintained.
type FieldType struct {
	Key     string   // canonical spelling used in "name:type" args
	Aliases []string // accepted alternative spellings
	GoType  string
	// GormType is the full struct-tag literal (backquoted in templates).
	GormType          string
	GQLType           string
	SQLTypePostgres   string
	SQLTypeMySQL      string
	SQLTypeSQLite     string
	SQLTypeSQLServer  string
	SQLTypeClickHouse string
}

// fieldTypes is ordered for display: docs tables render rows in this order.
// The first entry (string) doubles as the fallback for unknown type names,
// preserving ParseFields' long-standing default.
var fieldTypes = []FieldType{
	{
		Key:               "string",
		GoType:            "string",
		GormType:          `gorm:"not null"`,
		GQLType:           "String",
		SQLTypePostgres:   "VARCHAR(255) NOT NULL",
		SQLTypeMySQL:      "VARCHAR(255) NOT NULL",
		SQLTypeSQLite:     "TEXT NOT NULL",
		SQLTypeSQLServer:  "NVARCHAR(255) NOT NULL",
		SQLTypeClickHouse: "String",
	},
	{
		Key:               "text",
		GoType:            "string",
		GormType:          `gorm:"type:text;not null"`,
		GQLType:           "String",
		SQLTypePostgres:   "TEXT NOT NULL",
		SQLTypeMySQL:      "TEXT NOT NULL",
		SQLTypeSQLite:     "TEXT NOT NULL",
		SQLTypeSQLServer:  "NVARCHAR(MAX) NOT NULL",
		SQLTypeClickHouse: "String",
	},
	{
		Key:               "int",
		GoType:            "int",
		GormType:          `gorm:"not null"`,
		GQLType:           "Int",
		SQLTypePostgres:   "INTEGER NOT NULL",
		SQLTypeMySQL:      "INT NOT NULL",
		SQLTypeSQLite:     "INTEGER NOT NULL",
		SQLTypeSQLServer:  "INT NOT NULL",
		SQLTypeClickHouse: "Int32",
	},
	{
		Key:               "float",
		GoType:            "float64",
		GormType:          `gorm:"not null"`,
		GQLType:           "Float",
		SQLTypePostgres:   "DECIMAL(10,2) NOT NULL",
		SQLTypeMySQL:      "DECIMAL(10,2) NOT NULL",
		SQLTypeSQLite:     "REAL NOT NULL",
		SQLTypeSQLServer:  "DECIMAL(10,2) NOT NULL",
		SQLTypeClickHouse: "Float64",
	},
	{
		Key:               "bool",
		GoType:            "bool",
		GormType:          `gorm:"not null;default:false"`,
		GQLType:           "Boolean",
		SQLTypePostgres:   "BOOLEAN NOT NULL DEFAULT false",
		SQLTypeMySQL:      "TINYINT(1) NOT NULL DEFAULT 0",
		SQLTypeSQLite:     "INTEGER NOT NULL DEFAULT 0",
		SQLTypeSQLServer:  "BIT NOT NULL DEFAULT 0",
		SQLTypeClickHouse: "Bool",
	},
	{
		Key:               "uuid",
		GoType:            "uuid.UUID",
		GormType:          `gorm:"type:uuid;not null"`,
		GQLType:           "ID",
		SQLTypePostgres:   "UUID NOT NULL",
		SQLTypeMySQL:      "CHAR(36) NOT NULL",
		SQLTypeSQLite:     "TEXT NOT NULL",
		SQLTypeSQLServer:  "UNIQUEIDENTIFIER NOT NULL",
		SQLTypeClickHouse: "UUID",
	},
	{
		Key:               "time",
		Aliases:           []string{"datetime"},
		GoType:            "time.Time",
		GormType:          `gorm:"type:timestamp;not null"`,
		GQLType:           "DateTime",
		SQLTypePostgres:   "TIMESTAMP NOT NULL DEFAULT now()",
		SQLTypeMySQL:      "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		SQLTypeSQLite:     "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		SQLTypeSQLServer:  "DATETIME2 NOT NULL DEFAULT GETDATE()",
		SQLTypeClickHouse: "DateTime",
	},
}

// SupportedFieldTypes returns the registry in display order. The slice is a
// copy so callers cannot mutate the source of truth.
func SupportedFieldTypes() []FieldType {
	out := make([]FieldType, len(fieldTypes))
	copy(out, fieldTypes)
	return out
}

// lookupFieldType resolves a lowercased type name against keys and aliases.
func lookupFieldType(name string) (FieldType, bool) {
	for _, t := range fieldTypes {
		if t.Key == name || slices.Contains(t.Aliases, name) {
			return t, true
		}
	}
	return FieldType{}, false
}

// applyTo copies the type-derived columns onto a parsed Field, leaving the
// name-derived fields (Name/JSONName/SnakeName) untouched.
func (t FieldType) applyTo(f *Field) {
	f.GoType = t.GoType
	f.GormType = t.GormType
	f.GQLType = t.GQLType
	f.SQLTypePostgres = t.SQLTypePostgres
	f.SQLTypeMySQL = t.SQLTypeMySQL
	f.SQLTypeSQLite = t.SQLTypeSQLite
	f.SQLTypeSQLServer = t.SQLTypeSQLServer
	f.SQLTypeClickHouse = t.SQLTypeClickHouse
}
