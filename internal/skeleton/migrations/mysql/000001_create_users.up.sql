-- Users table for MySQL / MariaDB.
--
-- MySQL has no PL/pgSQL-style shared trigger functions (unlike Postgres'
-- skeleton migrations 000002/003/004 that create functions reused by every
-- table); each table inlines its own trigger bodies. Three invariants are
-- enforced at the DB layer so they hold for any client — application,
-- mysql CLI, admin tool, intruder with credentials:
--
--   - updated_at  → native column attribute `ON UPDATE CURRENT_TIMESTAMP`
--   - record_version → BEFORE UPDATE trigger sets NEW := OLD + 1
--   - is_deletable   → BEFORE DELETE trigger SIGNALs SQLSTATE '45000'
--
-- IMPORTANT — no `DELIMITER //` directives.
-- DELIMITER is a mysql-CLI directive that golang-migrate's driver does
-- NOT recognize (the server-side protocol doesn't understand it either).
-- golang-migrate auto-enables MultiStatements on the connection (see
-- v4.18.1/database/mysql/mysql.go:208), so multiple `;`-terminated
-- statements in one file work natively. BEGIN/END compound trigger
-- bodies are recognized as a single statement by the MySQL parser
-- regardless of inner `;`, so they don't need DELIMITER either.
CREATE TABLE `users` (
    `id` CHAR(36) PRIMARY KEY,
    `first_name` VARCHAR(255) NOT NULL,
    `other_names` VARCHAR(255) NOT NULL,
    `email` VARCHAR(255) NOT NULL UNIQUE,
    `password` VARCHAR(255) NOT NULL,
    `phone_number` VARCHAR(255) NOT NULL UNIQUE,
    `is_deletable` TINYINT(1) NOT NULL DEFAULT 1,
    `is_active` TINYINT(1) NOT NULL DEFAULT 1,
    `deleted_at` DATETIME NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `record_version` BIGINT NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TRIGGER increment_users_record_version_trigger
    BEFORE UPDATE ON `users`
    FOR EACH ROW
    SET NEW.record_version = OLD.record_version + 1;

CREATE TRIGGER avoid_deleting_not_deletable_users_trigger
    BEFORE DELETE ON `users`
    FOR EACH ROW
BEGIN
    IF OLD.is_deletable = 0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'This record is not deletable';
    END IF;
END;
