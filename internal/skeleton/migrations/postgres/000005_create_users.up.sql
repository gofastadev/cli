CREATE TABLE "users" (
    "id" uuid PRIMARY KEY,
    "first_name" character varying NOT NULL,
    "other_names" character varying NOT NULL,
    "email" character varying NOT NULL UNIQUE,
    "password" character varying NOT NULL,
    "phone_number" character varying NOT NULL UNIQUE,
    "is_deletable" Boolean NOT NULL DEFAULT true,
    "is_active" Boolean NOT NULL DEFAULT true,
    "deleted_at" TIMESTAMP,
    "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP DEFAULT NULL,
    "record_version" bigint NOT NULL DEFAULT 1
);

CREATE TRIGGER update_users_updated_at_trigger BEFORE
UPDATE
    ON "users" FOR EACH ROW EXECUTE FUNCTION update_updated_at_column_function();

CREATE TRIGGER avoid_deleting_record_with_is_deletable_equal_to_false_function_on_users_trigger BEFORE
DELETE
    ON "users" FOR EACH ROW EXECUTE FUNCTION avoid_deleting_record_with_is_deletable_equal_to_false_function();

CREATE TRIGGER increment_record_version_column_function_on_users_trigger BEFORE
UPDATE
    ON "users" FOR EACH ROW EXECUTE FUNCTION increment_record_version_column_function();
