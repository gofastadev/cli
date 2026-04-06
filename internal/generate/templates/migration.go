package templates

// --- PostgreSQL ---

var MigUpPostgres = `CREATE TABLE IF NOT EXISTS {{.PluralSnake}} (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
{{- range .Fields}}
    {{.SnakeName}} {{.SQLType}},
{{- end}}
    is_active BOOLEAN NOT NULL DEFAULT true,
    is_deletable BOOLEAN NOT NULL DEFAULT true,
    record_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    deleted_at TIMESTAMP
);

CREATE TRIGGER update_{{.PluralSnake}}_updated_at
    BEFORE UPDATE ON {{.PluralSnake}}
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER increment_{{.PluralSnake}}_record_version
    BEFORE UPDATE ON {{.PluralSnake}}
    FOR EACH ROW EXECUTE FUNCTION increment_record_version();
`

var MigDownPostgres = `DROP TRIGGER IF EXISTS increment_{{.PluralSnake}}_record_version ON {{.PluralSnake}};
DROP TRIGGER IF EXISTS update_{{.PluralSnake}}_updated_at ON {{.PluralSnake}};
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- MySQL / MariaDB ---

var MigUpMySQL = `CREATE TABLE IF NOT EXISTS {{.PluralSnake}} (
    id CHAR(36) PRIMARY KEY,
{{- range .Fields}}
    {{.SnakeName}} {{.SQLType}},
{{- end}}
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    is_deletable TINYINT(1) NOT NULL DEFAULT 1,
    record_version INT NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Note: record_version auto-increment requires application-level handling or a trigger.
-- MySQL trigger for record_version:
DELIMITER //
CREATE TRIGGER increment_{{.PluralSnake}}_record_version
    BEFORE UPDATE ON {{.PluralSnake}}
    FOR EACH ROW
BEGIN
    SET NEW.record_version = OLD.record_version + 1;
END//
DELIMITER ;
`

var MigDownMySQL = `DROP TRIGGER IF EXISTS increment_{{.PluralSnake}}_record_version;
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- SQLite ---

var MigUpSQLite = `CREATE TABLE IF NOT EXISTS {{.PluralSnake}} (
    id TEXT PRIMARY KEY,
{{- range .Fields}}
    {{.SnakeName}} {{.SQLType}},
{{- end}}
    is_active INTEGER NOT NULL DEFAULT 1,
    is_deletable INTEGER NOT NULL DEFAULT 1,
    record_version INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

-- SQLite trigger for updated_at
CREATE TRIGGER update_{{.PluralSnake}}_updated_at
    AFTER UPDATE ON {{.PluralSnake}}
    FOR EACH ROW
BEGIN
    UPDATE {{.PluralSnake}} SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- SQLite trigger for record_version
CREATE TRIGGER increment_{{.PluralSnake}}_record_version
    AFTER UPDATE ON {{.PluralSnake}}
    FOR EACH ROW
BEGIN
    UPDATE {{.PluralSnake}} SET record_version = OLD.record_version + 1 WHERE id = NEW.id;
END;
`

var MigDownSQLite = `DROP TRIGGER IF EXISTS increment_{{.PluralSnake}}_record_version;
DROP TRIGGER IF EXISTS update_{{.PluralSnake}}_updated_at;
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- SQL Server ---

var MigUpSQLServer = `IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='{{.PluralSnake}}' AND xtype='U')
CREATE TABLE {{.PluralSnake}} (
    id UNIQUEIDENTIFIER PRIMARY KEY DEFAULT NEWID(),
{{- range .Fields}}
    {{.SnakeName}} {{.SQLType}},
{{- end}}
    is_active BIT NOT NULL DEFAULT 1,
    is_deletable BIT NOT NULL DEFAULT 1,
    record_version INT NOT NULL DEFAULT 1,
    created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
    updated_at DATETIME2 NOT NULL DEFAULT GETDATE(),
    deleted_at DATETIME2 NULL
);
GO

-- SQL Server trigger for updated_at and record_version
CREATE OR ALTER TRIGGER trg_{{.PluralSnake}}_before_update
ON {{.PluralSnake}}
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE {{.PluralSnake}}
    SET updated_at = GETDATE(),
        record_version = {{.PluralSnake}}.record_version + 1
    FROM {{.PluralSnake}}
    INNER JOIN inserted ON {{.PluralSnake}}.id = inserted.id;
END;
GO
`

var MigDownSQLServer = `DROP TRIGGER IF EXISTS trg_{{.PluralSnake}}_before_update;
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- ClickHouse ---

var MigUpClickHouse = `CREATE TABLE IF NOT EXISTS {{.PluralSnake}} (
    id UUID DEFAULT generateUUIDv4(),
{{- range .Fields}}
    {{.SnakeName}} {{.SQLType}},
{{- end}}
    is_active Bool DEFAULT true,
    is_deletable Bool DEFAULT true,
    record_version Int32 DEFAULT 1,
    created_at DateTime DEFAULT now(),
    updated_at DateTime DEFAULT now(),
    deleted_at Nullable(DateTime)
) ENGINE = MergeTree()
ORDER BY id;
`

var MigDownClickHouse = `DROP TABLE IF EXISTS {{.PluralSnake}};
`
