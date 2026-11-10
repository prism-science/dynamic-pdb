-- Published revisions keep their files (B1b): see V20261110170100 for the rules and rollback.
--
-- This migration: model revision file links are written once, and a published model revision
-- gets no new files. Re-inserting a link it already has is allowed: it changes nothing (callers
-- use ON CONFLICT DO NOTHING). The revision row is locked FOR SHARE before its state is read, so
-- a link cannot slip in while another transaction is publishing the revision.
SET LOCAL lock_timeout = '5s';

-- Shared by every file link table (170200-170500).
CREATE FUNCTION file_links_written_once() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '%: file links are written once; % is not allowed', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION model_revision_artifacts_before_publish() RETURNS trigger AS $$
DECLARE
    revision_state text;
BEGIN
    SELECT state INTO revision_state FROM model_revisions WHERE id = NEW.model_revision_id FOR SHARE;
    IF revision_state IN ('active', 'archived') AND NOT EXISTS (
        SELECT 1 FROM model_revision_artifacts
        WHERE model_revision_id = NEW.model_revision_id AND artifact_id = NEW.artifact_id
    ) THEN
        RAISE EXCEPTION 'model_revision_artifacts: model revision % is published; its files cannot change', NEW.model_revision_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER model_revision_artifacts_before_publish
    BEFORE INSERT ON model_revision_artifacts
    FOR EACH ROW EXECUTE FUNCTION model_revision_artifacts_before_publish();

CREATE TRIGGER model_revision_artifacts_written_once
    BEFORE UPDATE OR DELETE ON model_revision_artifacts
    FOR EACH ROW EXECUTE FUNCTION file_links_written_once();

CREATE TRIGGER model_revision_artifacts_not_truncated
    BEFORE TRUNCATE ON model_revision_artifacts
    FOR EACH STATEMENT EXECUTE FUNCTION file_links_written_once();
