package templates

// --- PostgreSQL ---

// MigUpPostgres is the up-migration template for Postgres.
//
// Trigger naming + functions MUST match what the skeleton's
// foundational migrations (db/migrations/0000{2,3,4}_*) created:
//
//   - update_updated_at_column_function()                         — set updated_at = NOW() on UPDATE
//   - avoid_deleting_record_with_is_deletable_equal_to_false_function() — RAISE EXCEPTION on DELETE when is_deletable=false
//   - increment_record_version_column_function()                  — record_version := OLD.record_version + 1 on UPDATE
//
// These are intentionally DB-level (not Go-level) guards so the
// invariants hold for any client touching the database — admin
// tools, other services, intruders with psql, raw SQL via
// `db.Exec`, etc. — not just code that goes through GORM. Keep the
// trigger naming convention (`<verb>_<plural>_<noun>_trigger`) so
// `gofasta status` / migrate-down can find and drop them
// deterministically.
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

CREATE TRIGGER update_{{.PluralSnake}}_updated_at_trigger
    BEFORE UPDATE ON {{.PluralSnake}}
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column_function();

CREATE TRIGGER avoid_deleting_record_with_is_deletable_equal_to_false_function_on_{{.PluralSnake}}_trigger
    BEFORE DELETE ON {{.PluralSnake}}
    FOR EACH ROW EXECUTE FUNCTION avoid_deleting_record_with_is_deletable_equal_to_false_function();

CREATE TRIGGER increment_record_version_column_function_on_{{.PluralSnake}}_trigger
    BEFORE UPDATE ON {{.PluralSnake}}
    FOR EACH ROW EXECUTE FUNCTION increment_record_version_column_function();
`

// MigDownPostgres is the down-migration template for Postgres.
// Drops every trigger added by MigUpPostgres before dropping the
// table; trigger names match MigUpPostgres exactly.
var MigDownPostgres = `DROP TRIGGER IF EXISTS increment_record_version_column_function_on_{{.PluralSnake}}_trigger ON {{.PluralSnake}};
DROP TRIGGER IF EXISTS avoid_deleting_record_with_is_deletable_equal_to_false_function_on_{{.PluralSnake}}_trigger ON {{.PluralSnake}};
DROP TRIGGER IF EXISTS update_{{.PluralSnake}}_updated_at_trigger ON {{.PluralSnake}};
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- MySQL / MariaDB ---

// MigUpMySQL is the up-migration template for MySQL / MariaDB.
//
// MySQL has no shareable cross-table trigger function model (unlike
// Postgres' PL/pgSQL functions), so each table inlines its own trigger
// bodies. The not-deletable guard fires `SIGNAL SQLSTATE '45000'` so
// any client — application, mysql CLI, admin tool — gets the same
// rejection when attempting to DELETE a row with `is_deletable = 0`.
// `updated_at` is handled natively by the column attribute
// `ON UPDATE CURRENT_TIMESTAMP`, no trigger needed.
//
// IMPORTANT — no `DELIMITER //` directives.
// DELIMITER is a mysql-CLI directive that golang-migrate's driver
// doesn't recognize (the server-side protocol doesn't either).
// golang-migrate auto-enables MultiStatements on the connection (see
// v4.18.1/database/mysql/mysql.go:208), so multiple `;`-terminated
// statements in one file work natively. MySQL's parser recognizes
// BEGIN/END as a compound statement and reads through inner `;` to
// the matching END regardless of delimiter.
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

CREATE TRIGGER increment_{{.PluralSnake}}_record_version_trigger
    BEFORE UPDATE ON {{.PluralSnake}}
    FOR EACH ROW
    SET NEW.record_version = OLD.record_version + 1;

CREATE TRIGGER avoid_deleting_not_deletable_{{.PluralSnake}}_trigger
    BEFORE DELETE ON {{.PluralSnake}}
    FOR EACH ROW
BEGIN
    IF OLD.is_deletable = 0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'This record is not deletable';
    END IF;
END;
`

// MigDownMySQL is the down-migration template for MySQL / MariaDB.
// Drops every trigger added by MigUpMySQL before dropping the table.
var MigDownMySQL = `DROP TRIGGER IF EXISTS avoid_deleting_not_deletable_{{.PluralSnake}}_trigger;
DROP TRIGGER IF EXISTS increment_{{.PluralSnake}}_record_version_trigger;
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- SQLite ---

// MigUpSQLite is the up-migration template for SQLite.
//
// SQLite triggers can't call shared functions — each table inlines its
// own bodies. The not-deletable guard uses `WHEN OLD.is_deletable = 0`
// + `RAISE(ABORT, ...)` so the DELETE statement aborts with an error
// that any client (sqlite3 CLI, GUI tool, application) sees.
// `updated_at` and `record_version` use AFTER triggers that
// self-UPDATE the row to bump the columns post-write.
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

CREATE TRIGGER update_{{.PluralSnake}}_updated_at_trigger
    AFTER UPDATE ON {{.PluralSnake}}
    FOR EACH ROW
BEGIN
    UPDATE {{.PluralSnake}} SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER increment_{{.PluralSnake}}_record_version_trigger
    AFTER UPDATE ON {{.PluralSnake}}
    FOR EACH ROW
BEGIN
    UPDATE {{.PluralSnake}} SET record_version = OLD.record_version + 1 WHERE id = NEW.id;
END;

CREATE TRIGGER avoid_deleting_not_deletable_{{.PluralSnake}}_trigger
    BEFORE DELETE ON {{.PluralSnake}}
    FOR EACH ROW WHEN OLD.is_deletable = 0
BEGIN
    SELECT RAISE(ABORT, 'This record is not deletable');
END;
`

// MigDownSQLite is the down-migration template for SQLite.
// Drops every trigger added by MigUpSQLite before dropping the table.
var MigDownSQLite = `DROP TRIGGER IF EXISTS avoid_deleting_not_deletable_{{.PluralSnake}}_trigger;
DROP TRIGGER IF EXISTS increment_{{.PluralSnake}}_record_version_trigger;
DROP TRIGGER IF EXISTS update_{{.PluralSnake}}_updated_at_trigger;
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- SQL Server ---

// MigUpSQLServer is the up-migration template for Microsoft SQL Server.
//
// One AFTER UPDATE trigger handles both `updated_at` bump and
// `record_version` increment in a single pass; one INSTEAD OF DELETE
// trigger enforces the is_deletable invariant. Triggers fire for any
// client — sqlcmd, SSMS, application, intruder.
//
// IMPORTANT — no `GO` batch separators.
// golang-migrate's sqlserver driver has NO multi-statement / batch-
// splitter support — it sends the whole file as one TDS batch via
// Exec(). T-SQL forbids CREATE TRIGGER (or CREATE PROCEDURE / VIEW /
// FUNCTION) anywhere except the FIRST statement of a batch, so a
// naïve "CREATE TABLE; CREATE TRIGGER;" file would error out.
// Workaround: wrap each CREATE TRIGGER in `EXEC sp_executesql N'...'`
// so the trigger creation runs in its own internal batch (the
// standard pattern in FluentMigrator / EF Core for the same reason).
// Single quotes inside the trigger body escape as `”` per T-SQL.
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

EXEC sp_executesql N'
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
END';

EXEC sp_executesql N'
CREATE OR ALTER TRIGGER trg_{{.PluralSnake}}_avoid_not_deletable
ON {{.PluralSnake}}
INSTEAD OF DELETE
AS
BEGIN
    SET NOCOUNT ON;
    IF EXISTS (SELECT 1 FROM deleted WHERE is_deletable = 0)
        THROW 51000, ''This record is not deletable'', 1;
    DELETE FROM {{.PluralSnake}}
    WHERE id IN (SELECT id FROM deleted);
END';
`

// MigDownSQLServer is the down-migration template for Microsoft SQL Server.
// Drops every trigger added by MigUpSQLServer before dropping the table.
// Wrapped in sp_executesql for the same batch-isolation reason
// (DROP TRIGGER also requires "first in batch").
var MigDownSQLServer = `EXEC sp_executesql N'DROP TRIGGER IF EXISTS trg_{{.PluralSnake}}_avoid_not_deletable';
EXEC sp_executesql N'DROP TRIGGER IF EXISTS trg_{{.PluralSnake}}_before_update';
DROP TABLE IF EXISTS {{.PluralSnake}};
`

// --- ClickHouse ---

// MigUpClickHouse is the up-migration template for ClickHouse.
//
// ClickHouse does NOT support row-level triggers (it's an OLAP /
// append-mostly engine; the MergeTree family lacks the
// per-statement firing semantics PG/MySQL/SQLite/SQL Server provide).
// The three invariants — `updated_at` bump, `record_version`
// increment, and the not-deletable guard — are therefore enforced at
// the APPLICATION LAYER only when running on ClickHouse. A direct
// `clickhouse-client` DELETE bypasses these checks. Do not pick
// ClickHouse for tables where DB-level intruder protection is
// load-bearing.
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

// MigDownClickHouse is the down-migration template for ClickHouse.
var MigDownClickHouse = `DROP TABLE IF EXISTS {{.PluralSnake}};
`
