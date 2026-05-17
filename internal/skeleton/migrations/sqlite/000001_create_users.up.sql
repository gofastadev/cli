-- Users table for SQLite.
--
-- SQLite triggers cannot call shared functions; each table inlines its
-- own bodies. Three invariants enforced at the DB layer:
--
--   - updated_at  → AFTER UPDATE trigger self-UPDATEs the row
--   - record_version → AFTER UPDATE trigger self-UPDATEs the row
--   - is_deletable   → BEFORE DELETE trigger RAISE(ABORT) when blocked
--
-- UUIDs are stored as TEXT (SQLite's universal stringy storage); GORM's
-- uuid.UUID round-trips through that via its TextMarshaler.
CREATE TABLE "users" (
    "id" TEXT PRIMARY KEY,
    "first_name" TEXT NOT NULL,
    "other_names" TEXT NOT NULL,
    "email" TEXT NOT NULL UNIQUE,
    "password" TEXT NOT NULL,
    "phone_number" TEXT NOT NULL UNIQUE,
    "is_deletable" INTEGER NOT NULL DEFAULT 1,
    "is_active" INTEGER NOT NULL DEFAULT 1,
    "deleted_at" DATETIME,
    "created_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "record_version" INTEGER NOT NULL DEFAULT 1
);

CREATE TRIGGER update_users_updated_at_trigger
    AFTER UPDATE ON "users"
    FOR EACH ROW
BEGIN
    UPDATE "users" SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER increment_users_record_version_trigger
    AFTER UPDATE ON "users"
    FOR EACH ROW
BEGIN
    UPDATE "users" SET record_version = OLD.record_version + 1 WHERE id = NEW.id;
END;

CREATE TRIGGER avoid_deleting_not_deletable_users_trigger
    BEFORE DELETE ON "users"
    FOR EACH ROW WHEN OLD.is_deletable = 0
BEGIN
    SELECT RAISE(ABORT, 'This record is not deletable');
END;
