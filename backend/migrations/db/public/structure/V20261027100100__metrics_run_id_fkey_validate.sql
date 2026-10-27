-- Validates the foreign key added NOT VALID by the previous migration. It takes SHARE UPDATE
-- EXCLUSIVE on metrics and ROW SHARE on runs, so reads and writes continue while it scans
-- metrics once; every existing run_id is NULL at this point.
ALTER TABLE metrics VALIDATE CONSTRAINT metrics_run_id_fkey;
