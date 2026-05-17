CREATE
OR REPLACE FUNCTION update_updated_at_column_function() RETURNS TRIGGER AS $$ BEGIN NEW.updated_at := NOW();

RETURN NEW;

END;

$$ LANGUAGE plpgsql;
