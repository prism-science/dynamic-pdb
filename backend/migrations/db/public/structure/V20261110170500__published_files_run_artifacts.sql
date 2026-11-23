-- Published revisions keep their files (B1b): see V20261110170100 for the rules and rollback.
--
-- This migration: run file links are written once, and a run that a published model revision
-- uses gets no new files. Re-inserting a link it already has is allowed.
--
-- Locks, in this order: the run row FOR SHARE, which waits for a transaction linking the run to
-- a revision (that takes it FOR NO KEY UPDATE, V20261110170400) and makes such a transaction
-- wait for this one; then the model revisions that use the run FOR SHARE, which waits for a
-- transaction publishing one of them. Each later statement reads the committed links and states.
-- The lookup by run is served by model_revision_runs_run_id_idx (V20261110170000).
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION run_artifacts_before_publish() RETURNS trigger AS $$
BEGIN
    PERFORM 1 FROM runs WHERE id = NEW.run_id FOR SHARE;
    PERFORM 1 FROM model_revisions
    WHERE id IN (SELECT model_revision_id FROM model_revision_runs WHERE run_id = NEW.run_id)
    FOR SHARE;
    IF EXISTS (
        SELECT 1 FROM model_revision_runs
        JOIN model_revisions ON model_revisions.id = model_revision_runs.model_revision_id
        WHERE model_revision_runs.run_id = NEW.run_id
          AND model_revisions.state IN ('active', 'archived')
    ) AND NOT EXISTS (
        SELECT 1 FROM run_artifacts
        WHERE run_id = NEW.run_id AND artifact_id = NEW.artifact_id AND direction = NEW.direction
    ) THEN
        RAISE EXCEPTION 'run_artifacts: run % belongs to a published model revision; its files cannot change', NEW.run_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER run_artifacts_before_publish
    BEFORE INSERT ON run_artifacts
    FOR EACH ROW EXECUTE FUNCTION run_artifacts_before_publish();

CREATE TRIGGER run_artifacts_written_once
    BEFORE UPDATE OR DELETE ON run_artifacts
    FOR EACH ROW EXECUTE FUNCTION file_links_written_once();

CREATE TRIGGER run_artifacts_not_truncated
    BEFORE TRUNCATE ON run_artifacts
    FOR EACH STATEMENT EXECUTE FUNCTION file_links_written_once();
