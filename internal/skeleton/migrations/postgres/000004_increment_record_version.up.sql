CREATE OR REPLACE FUNCTION increment_record_version_column_function() RETURNS TRIGGER AS $$
BEGIN
    NEW.record_version := OLD.record_version + 1;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
