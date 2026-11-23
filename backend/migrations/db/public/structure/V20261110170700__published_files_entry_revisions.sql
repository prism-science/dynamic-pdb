-- Published revisions keep their files (B1b): see V20261110170100 for the rules and rollback.
--
-- This migration: a published entry revision only moves from active to archived. Without it a
-- published revision could be taken back to a draft, given new files, and published again.
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION entry_revisions_stay_published() RETURNS trigger AS $$
BEGIN
    IF OLD.state IN ('active', 'archived') AND NEW.state IS DISTINCT FROM OLD.state
        AND NOT (OLD.state = 'active' AND NEW.state = 'archived') THEN
        RAISE EXCEPTION 'entry_revisions: entry revision % is published; it can only move from active to archived, not from % to %',
            OLD.id, OLD.state, NEW.state
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER entry_revisions_stay_published
    BEFORE UPDATE OF state ON entry_revisions
    FOR EACH ROW EXECUTE FUNCTION entry_revisions_stay_published();
