-- Validates the two CHECK constraints the previous migration added NOT VALID. VALIDATE takes
-- SHARE UPDATE EXCLUSIVE on data_sync_jobs, so the sync and new entries keep writing while it scans
-- the table once; every row passes, since the new columns are all NULL at this point.
ALTER TABLE data_sync_jobs VALIDATE CONSTRAINT data_sync_jobs_archive_version_check;
ALTER TABLE data_sync_jobs VALIDATE CONSTRAINT data_sync_jobs_stop_check;
