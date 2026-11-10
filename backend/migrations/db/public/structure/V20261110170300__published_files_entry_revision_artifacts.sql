-- Published revisions keep their files (B1b): see V20261110170100 for the rules and rollback.
--
-- This migration: entry revision file links are written once, and a published entry revision
-- gets no new files. Re-inserting a link it already has is allowed. The revision row is locked
-- FOR SHARE before its state is read.
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION entry_revision_artifacts_before_publish() RETURNS trigger AS $$
DECLARE
    revision_state text;
BEGIN
    SELECT state INTO revision_state FROM entry_revisions WHERE id = NEW.entry_revision_id FOR SHARE;
    IF revision_state IN ('active', 'archived') AND NOT EXISTS (
        SELECT 1 FROM entry_revision_artifacts
        WHERE entry_revision_id = NEW.entry_revision_id AND artifact_id = NEW.artifact_id
    ) THEN
        RAISE EXCEPTION 'entry_revision_artifacts: entry revision % is published; its files cannot change', NEW.entry_revision_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER entry_revision_artifacts_before_publish
    BEFORE INSERT ON entry_revision_artifacts
    FOR EACH ROW EXECUTE FUNCTION entry_revision_artifacts_before_publish();

CREATE TRIGGER entry_revision_artifacts_written_once
    BEFORE UPDATE OR DELETE ON entry_revision_artifacts
    FOR EACH ROW EXECUTE FUNCTION file_links_written_once();

CREATE TRIGGER entry_revision_artifacts_not_truncated
    BEFORE TRUNCATE ON entry_revision_artifacts
    FOR EACH STATEMENT EXECUTE FUNCTION file_links_written_once();
