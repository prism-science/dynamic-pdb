-- Published revisions keep their files (B1b): see V20261110170100 for the rules and rollback.
--
-- This migration: a published model revision keeps its primary file, and it only moves from
-- active to archived. Without the second rule a published revision could be taken back to a
-- draft, given new files, and published again.
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION model_revisions_stay_published() RETURNS trigger AS $$
BEGIN
    IF OLD.state NOT IN ('active', 'archived') THEN
        RETURN NEW;
    END IF;
    IF NEW.primary_artifact_id IS DISTINCT FROM OLD.primary_artifact_id THEN
        RAISE EXCEPTION 'model_revisions: model revision % is published; its primary file cannot change', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (OLD.state = 'active' AND NEW.state = 'archived') THEN
        RAISE EXCEPTION 'model_revisions: model revision % is published; it can only move from active to archived, not from % to %',
            OLD.id, OLD.state, NEW.state
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER model_revisions_stay_published
    BEFORE UPDATE OF primary_artifact_id, state ON model_revisions
    FOR EACH ROW EXECUTE FUNCTION model_revisions_stay_published();
