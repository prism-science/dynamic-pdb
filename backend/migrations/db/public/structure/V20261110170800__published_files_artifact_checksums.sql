-- Published revisions keep their files (B1b): see V20261110170100 for the rules and rollback.
--
-- This migration: once the checksum job has read a file's bytes (status verified or mismatch),
-- its result is kept. A selection names the computed SHA-256 of a file added without one, so
-- that SHA-256, the size, the status and the time of the check never change, and the row is
-- never deleted. Pending and failed rows stay writable: the job retries them. Where our copy is
-- kept (stored_uri, copy_status) and the retry fields stay writable too.
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION artifact_checksums_keep_result() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'artifact_checksums: checksum results cannot be truncated; saved selections name them'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.status NOT IN ('verified', 'mismatch') THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'artifact_checksums: the checksum result of artifact % cannot be deleted; saved selections name it', OLD.artifact_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF (NEW.status, NEW.sha256, NEW.size_bytes, NEW.checked_at)
        IS DISTINCT FROM (OLD.status, OLD.sha256, OLD.size_bytes, OLD.checked_at) THEN
        RAISE EXCEPTION 'artifact_checksums: the checksum result of artifact % is recorded; it cannot change', OLD.artifact_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER artifact_checksums_keep_result
    BEFORE UPDATE OR DELETE ON artifact_checksums
    FOR EACH ROW EXECUTE FUNCTION artifact_checksums_keep_result();

CREATE TRIGGER artifact_checksums_keep_results
    BEFORE TRUNCATE ON artifact_checksums
    FOR EACH STATEMENT EXECUTE FUNCTION artifact_checksums_keep_result();
