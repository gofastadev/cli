package generate

import "strings"

// ParseFields converts CLI args like "name:string price:float" into typed Fields.
func ParseFields(args []string) []Field {
	var fields []Field
	for _, arg := range args {
		parts := strings.SplitN(arg, ":", 2)
		if len(parts) != 2 {
			continue
		}
		f := Field{
			Name:      toPascalCase(parts[0]),
			JSONName:  toCamelCase(parts[0]),
			SnakeName: toSnakeCase(parts[0]),
		}

		switch strings.ToLower(parts[1]) {
		case "string":
			f.GoType = "string"
			f.GormType = `gorm:"not null"`
			f.GQLType = "String"
			f.SQLTypePostgres = "VARCHAR(255) NOT NULL"
			f.SQLTypeMySQL = "VARCHAR(255) NOT NULL"
			f.SQLTypeSQLite = "TEXT NOT NULL"
			f.SQLTypeSQLServer = "NVARCHAR(255) NOT NULL"
			f.SQLTypeClickHouse = "String"

		case "text":
			f.GoType = "string"
			f.GormType = `gorm:"type:text;not null"`
			f.GQLType = "String"
			f.SQLTypePostgres = "TEXT NOT NULL"
			f.SQLTypeMySQL = "TEXT NOT NULL"
			f.SQLTypeSQLite = "TEXT NOT NULL"
			f.SQLTypeSQLServer = "NVARCHAR(MAX) NOT NULL"
			f.SQLTypeClickHouse = "String"

		case "int":
			f.GoType = "int"
			f.GormType = `gorm:"not null"`
			f.GQLType = "Int"
			f.SQLTypePostgres = "INTEGER NOT NULL"
			f.SQLTypeMySQL = "INT NOT NULL"
			f.SQLTypeSQLite = "INTEGER NOT NULL"
			f.SQLTypeSQLServer = "INT NOT NULL"
			f.SQLTypeClickHouse = "Int32"

		case "float":
			f.GoType = "float64"
			f.GormType = `gorm:"not null"`
			f.GQLType = "Float"
			f.SQLTypePostgres = "DECIMAL(10,2) NOT NULL"
			f.SQLTypeMySQL = "DECIMAL(10,2) NOT NULL"
			f.SQLTypeSQLite = "REAL NOT NULL"
			f.SQLTypeSQLServer = "DECIMAL(10,2) NOT NULL"
			f.SQLTypeClickHouse = "Float64"

		case "bool":
			f.GoType = "bool"
			f.GormType = `gorm:"not null;default:false"`
			f.GQLType = "Boolean"
			f.SQLTypePostgres = "BOOLEAN NOT NULL DEFAULT false"
			f.SQLTypeMySQL = "TINYINT(1) NOT NULL DEFAULT 0"
			f.SQLTypeSQLite = "INTEGER NOT NULL DEFAULT 0"
			f.SQLTypeSQLServer = "BIT NOT NULL DEFAULT 0"
			f.SQLTypeClickHouse = "Bool"

		case "uuid":
			f.GoType = "uuid.UUID"
			f.GormType = `gorm:"type:uuid;not null"`
			f.GQLType = "ID"
			f.SQLTypePostgres = "UUID NOT NULL"
			f.SQLTypeMySQL = "CHAR(36) NOT NULL"
			f.SQLTypeSQLite = "TEXT NOT NULL"
			f.SQLTypeSQLServer = "UNIQUEIDENTIFIER NOT NULL"
			f.SQLTypeClickHouse = "UUID"

		case "time", "datetime":
			f.GoType = "time.Time"
			f.GormType = `gorm:"type:timestamp;not null"`
			f.GQLType = "DateTime"
			f.SQLTypePostgres = "TIMESTAMP NOT NULL DEFAULT now()"
			f.SQLTypeMySQL = "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"
			f.SQLTypeSQLite = "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"
			f.SQLTypeSQLServer = "DATETIME2 NOT NULL DEFAULT GETDATE()"
			f.SQLTypeClickHouse = "DateTime"

		default:
			f.GoType = "string"
			f.GormType = `gorm:"not null"`
			f.GQLType = "String"
			f.SQLTypePostgres = "VARCHAR(255) NOT NULL"
			f.SQLTypeMySQL = "VARCHAR(255) NOT NULL"
			f.SQLTypeSQLite = "TEXT NOT NULL"
			f.SQLTypeSQLServer = "NVARCHAR(255) NOT NULL"
			f.SQLTypeClickHouse = "String"
		}

		fields = append(fields, f)
	}
	return fields
}
