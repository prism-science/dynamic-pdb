-- Published revisions keep their files (B1b, researcher item 9), migrations 170100-170800.
--
-- A saved selection pins model and entry revisions; through them it pins each file's ID and
-- recorded SHA-256. That holds only if a published revision's files never change, and the app
-- already works that way:
--   - artifacts are written once and never updated or deleted;
--   - files are linked to a revision only while it is being created (pending or in review);
--     sync links them to its new revision before activating it;
--   - a run's files are linked only when the run is created;
--   - primary_artifact_id changes only in drafts (pending or rejected);
--   - a published revision only moves from active to archived;
--   - the checksum job records each file's result once.
-- These triggers make the database refuse anything else, so a later change cannot alter what a
-- saved selection, a download manifest or an earlier revision names. They also enforce the
-- researchers' "never overwrite a recorded checksum" (items 8, 14).
--
-- "Published" = state active or archived (archived = replaced by a newer revision, or deleted
-- with its entry). Pending, in_review and rejected revisions are drafts and stay writable.
--
-- Every refusal raises SQLSTATE 23001 (restrict_violation) naming the table and the rule.
-- One table per migration: each holds one table's SHARE ROW EXCLUSIVE lock for milliseconds and
-- never waits for a second lock while holding one, so it cannot deadlock with app transactions.
--
-- A deliberate repair or takedown by an operator disables the trigger for its own transaction
-- (BEGIN; ALTER TABLE <table> DISABLE TRIGGER <trigger>; ...; ALTER TABLE <table> ENABLE TRIGGER
-- <trigger>; COMMIT;) and is recorded in the change log. A migration that backfills a new
-- artifacts column does the same.
-- Rollback of all eight migrations: DROP the triggers and their functions; no data is involved.
--
-- This migration: an artifact names one file, so everything but metadata (which describes the
-- file, not its bytes) is written once, every column added later included.
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION artifacts_keep_recorded_file() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'artifacts: artifacts cannot be truncated; revisions and saved selections name them'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'artifacts: artifact % cannot be deleted; revisions and saved selections name it', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF to_jsonb(NEW) - 'metadata' IS DISTINCT FROM to_jsonb(OLD) - 'metadata' THEN
        RAISE EXCEPTION 'artifacts: artifact % keeps its recorded file; only metadata can change', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER artifacts_keep_recorded_file
    BEFORE UPDATE OR DELETE ON artifacts
    FOR EACH ROW EXECUTE FUNCTION artifacts_keep_recorded_file();

CREATE TRIGGER artifacts_keep_recorded_files
    BEFORE TRUNCATE ON artifacts
    FOR EACH STATEMENT EXECUTE FUNCTION artifacts_keep_recorded_file();
