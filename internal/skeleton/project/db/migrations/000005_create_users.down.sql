DROP TRIGGER IF EXISTS increment_record_version_column_function_on_users_trigger ON "users"
DROP TRIGGER IF EXISTS avoid_deleting_record_with_is_deletable_equal_to_false_function_on_users_trigger ON "users";
DROP TRIGGER IF EXISTS update_users_updated_at_trigger ON "users";

DROP TABLE IF EXISTS "users";
