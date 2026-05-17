-- Users table for Microsoft SQL Server.
--
-- IMPORTANT — no `GO` batch separators.
-- golang-migrate's sqlserver driver has NO multi-statement / batch-
-- splitter support: it sends the whole file to the server as ONE TDS
-- batch via Exec(). T-SQL forbids CREATE TRIGGER (and CREATE
-- PROCEDURE / VIEW / FUNCTION) anywhere except as the FIRST statement
-- in a batch — so a naïve `CREATE TABLE ...; CREATE TRIGGER ...;`
-- file would error out with
--   "CREATE TRIGGER must be the only statement in the batch".
--
-- Workaround: wrap each CREATE TRIGGER in an `EXEC sp_executesql
-- N'...'` call. sp_executesql runs the dynamic SQL string as its own
-- internal batch, where the CREATE TRIGGER IS the only statement.
-- This is the standard pattern in FluentMigrator / EF Core for the
-- same reason. Single quotes inside the trigger body are escaped as
-- `''` per T-SQL string-literal rules.
--
-- DB-level invariants (hold for every client, including sqlcmd /
-- SSMS / intruders):
--   - updated_at + record_version → AFTER UPDATE trigger updates row
--   - is_deletable                → INSTEAD OF DELETE trigger THROWs
CREATE TABLE users (
    id UNIQUEIDENTIFIER PRIMARY KEY DEFAULT NEWID(),
    first_name NVARCHAR(255) NOT NULL,
    other_names NVARCHAR(255) NOT NULL,
    email NVARCHAR(255) NOT NULL UNIQUE,
    password NVARCHAR(255) NOT NULL,
    phone_number NVARCHAR(255) NOT NULL UNIQUE,
    is_deletable BIT NOT NULL DEFAULT 1,
    is_active BIT NOT NULL DEFAULT 1,
    deleted_at DATETIME2 NULL,
    created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
    updated_at DATETIME2 NOT NULL DEFAULT GETDATE(),
    record_version BIGINT NOT NULL DEFAULT 1
);

EXEC sp_executesql N'
CREATE TRIGGER trg_users_before_update
ON users
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE users
    SET updated_at = GETDATE(),
        record_version = users.record_version + 1
    FROM users
    INNER JOIN inserted ON users.id = inserted.id;
END';

EXEC sp_executesql N'
CREATE TRIGGER trg_users_avoid_not_deletable
ON users
INSTEAD OF DELETE
AS
BEGIN
    SET NOCOUNT ON;
    IF EXISTS (SELECT 1 FROM deleted WHERE is_deletable = 0)
        THROW 51000, ''This record is not deletable'', 1;
    DELETE FROM users
    WHERE id IN (SELECT id FROM deleted);
END';
