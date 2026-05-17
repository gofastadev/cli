-- Users table for Microsoft SQL Server.
--
-- One AFTER UPDATE trigger handles both updated_at and record_version
-- in a single pass; one INSTEAD OF DELETE trigger enforces the
-- is_deletable invariant. Triggers fire for any client — application,
-- sqlcmd, SSMS, intruder with credentials.
--
-- Note on `migrate`-tool batch semantics: golang-migrate's sqlserver
-- driver splits on `GO` batch markers; each statement below must end
-- with `GO` on its own line to land in the right execution batch.
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
GO

CREATE OR ALTER TRIGGER trg_users_before_update
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
END;
GO

CREATE OR ALTER TRIGGER trg_users_avoid_not_deletable
ON users
INSTEAD OF DELETE
AS
BEGIN
    SET NOCOUNT ON;
    IF EXISTS (SELECT 1 FROM deleted WHERE is_deletable = 0)
        THROW 51000, 'This record is not deletable', 1;
    DELETE FROM users
    WHERE id IN (SELECT id FROM deleted);
END;
GO
