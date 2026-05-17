CREATE
OR REPLACE FUNCTION avoid_deleting_record_with_is_deletable_equal_to_false_function() RETURNS trigger AS $$
BEGIN IF
    OLD.is_deletable = false THEN RAISE EXCEPTION 'This record is not deletable';
END IF;
RETURN OLD;
END;
$$ LANGUAGE plpgsql;
