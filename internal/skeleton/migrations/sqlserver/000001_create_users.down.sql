-- Drop triggers via EXEC sp_executesql so each runs in its own batch
-- (DROP TRIGGER has the same "must be first in batch" rule in SQL
-- Server). See 000001_create_users.up.sql for why GO directives
-- aren't usable through golang-migrate's sqlserver driver.
EXEC sp_executesql N'DROP TRIGGER IF EXISTS trg_users_avoid_not_deletable';
EXEC sp_executesql N'DROP TRIGGER IF EXISTS trg_users_before_update';
DROP TABLE IF EXISTS users;
