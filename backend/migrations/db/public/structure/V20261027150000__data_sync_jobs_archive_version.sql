-- The archive sync now runs only when RCSB has a newer version of an entry than the one the job
-- last applied (researcher answer Q4.1). Each job records that version: its ordinal in the
-- entry's revision history, its major.minor number and its date. A job that has not run since
-- this migration has none; its first run records RCSB's current version and changes nothing
-- (decision D2).
--
-- A job also stops for good when RCSB obsoletes or never released its entry, instead of being
-- deleted, so the reason stays visible.
--
-- The new columns are nullable with no default. The two CHECK constraints scan the table once
-- (about 146,000 rows in production) while ADD COLUMN holds ACCESS EXCLUSIVE; lock_timeout makes
-- the migration fail cleanly (nothing applied, rerun it) instead of queueing behind the sync.
SET LOCAL lock_timeout = '5s';

ALTER TABLE data_sync_jobs
    ADD COLUMN archive_revision_ordinal integer,
    ADD COLUMN archive_version text,
    ADD COLUMN archive_revision_date timestamptz,
    ADD COLUMN stopped_at timestamptz,
    ADD COLUMN stop_reason text,
    ADD CONSTRAINT data_sync_jobs_archive_version_check
        CHECK ((archive_revision_ordinal IS NULL) = (archive_version IS NULL)),
    ADD CONSTRAINT data_sync_jobs_stop_check
        CHECK ((stopped_at IS NULL) = (stop_reason IS NULL));

-- The sync used to delete a job when RCSB answered 404, so archive entries that RCSB obsoleted
-- lost their job and show stale data with no label (28 entries in production, all obsolete at
-- RCSB). Give every active archive entry without an entry job a job that is due now; its first
-- run labels the entry obsolete and stops the job.
INSERT INTO data_sync_jobs(entry_id, model_id, scheduled_at)
SELECT revision.entry_id, NULL, now()
FROM entry_revisions revision
JOIN entries entry ON entry.id = revision.entry_id
WHERE revision.state = 'active'
  AND entry.state = 'active'
  AND revision.metadata -> 'external_refs' ->> 'pdb' IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM data_sync_jobs job
      WHERE job.entry_id = revision.entry_id
        AND job.model_id IS NULL
  );
