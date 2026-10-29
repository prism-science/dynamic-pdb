-- copy_status says whether the checksum job kept a copy of an artifact's file in our bucket,
-- and if not, why (OPS-C). Until now the reason went only to a log line, so a verified row
-- without stored_uri looked the same whether the file was in our bucket already, over the copy
-- limit, or its copy failed.
--
-- copied (our copy is at stored_uri), own_storage (the file is in our bucket already, so it
-- needs no copy), over_limit (larger than the job's copy limit), mismatch (the bytes differ from
-- the declared values, so the copy was discarded), copy_failed (the copy could not be started or
-- stored), copy_off (copying external files is turned off). It is set once the bytes were read:
-- verified and mismatch rows have one, pending and failed rows have none.
--
-- The column is nullable with no default, so ADD COLUMN changes only the catalog and rewrites no
-- row; almost all of production's ~568,000 rows are pending and stay NULL. The CHECK is NOT
-- VALID, so adding it scans nothing; it applies to every row written from now on. The next
-- migration fills the rows already checked, and the one after validates the CHECK without
-- blocking writes. ALTER TABLE takes ACCESS EXCLUSIVE for milliseconds; lock_timeout makes the
-- migration fail cleanly (nothing applied, rerun it) instead of queueing behind a long
-- transaction.
SET LOCAL lock_timeout = '5s';

-- Each branch tests copy_status IS NOT NULL first: a NULL in IN (...) makes the CHECK unknown,
-- which Postgres accepts.
ALTER TABLE artifact_checksums
    ADD COLUMN copy_status text,
    ADD CONSTRAINT artifact_checksums_copy_status_check CHECK (
        CASE status
            WHEN 'verified' THEN copy_status IS NOT NULL
                AND copy_status IN ('copied', 'own_storage', 'over_limit', 'copy_failed', 'copy_off')
                AND (copy_status = 'copied') = (stored_uri IS NOT NULL)
            WHEN 'mismatch' THEN copy_status IS NOT NULL
                AND copy_status IN ('own_storage', 'mismatch')
                AND stored_uri IS NULL
            ELSE copy_status IS NULL
        END
    ) NOT VALID;
