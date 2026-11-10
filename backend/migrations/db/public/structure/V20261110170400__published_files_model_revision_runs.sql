-- Published revisions keep their files (B1b): see V20261110170100 for the rules and rollback.
--
-- This migration: model revision run links are written once, and a published model revision
-- gets no new runs. Re-inserting a link it already has is allowed. The revision row is locked
-- FOR SHARE before its state is read.
--
-- Linking a run also locks the run row FOR NO KEY UPDATE. Adding a file to a run locks it FOR
-- SHARE (V20261110170500), so the two wait for each other: a file added to a run that is not
-- linked yet commits before the run joins a revision, or sees the link and its revision's
-- state. Without it, a file could be added to a run while another transaction links that run
-- to a draft and publishes it, and the file would appear in a published revision afterwards.
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION model_revision_runs_before_publish() RETURNS trigger AS $$
DECLARE
    revision_state text;
BEGIN
    PERFORM 1 FROM runs WHERE id = NEW.run_id FOR NO KEY UPDATE;
    SELECT state INTO revision_state FROM model_revisions WHERE id = NEW.model_revision_id FOR SHARE;
    IF revision_state IN ('active', 'archived') AND NOT EXISTS (
        SELECT 1 FROM model_revision_runs
        WHERE model_revision_id = NEW.model_revision_id AND run_id = NEW.run_id
    ) THEN
        RAISE EXCEPTION 'model_revision_runs: model revision % is published; its runs cannot change', NEW.model_revision_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER model_revision_runs_before_publish
    BEFORE INSERT ON model_revision_runs
    FOR EACH ROW EXECUTE FUNCTION model_revision_runs_before_publish();

CREATE TRIGGER model_revision_runs_written_once
    BEFORE UPDATE OR DELETE ON model_revision_runs
    FOR EACH ROW EXECUTE FUNCTION file_links_written_once();

CREATE TRIGGER model_revision_runs_not_truncated
    BEFORE TRUNCATE ON model_revision_runs
    FOR EACH STATEMENT EXECUTE FUNCTION file_links_written_once();
