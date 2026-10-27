-- Metrics record the software that produced them: software_name and software_version,
-- or run_id, the run that produced the value.
--
-- The three columns are nullable with no default, so ADD COLUMN changes only the catalog.
-- Existing metrics stay without software: a migration must not invent it. The archive
-- sync fills the software of archive metrics from RCSB.
--
-- The foreign key is NOT VALID, so adding it scans nothing; the next migration validates it.
--
-- ADD COLUMN takes ACCESS EXCLUSIVE on metrics and the foreign key SHARE ROW EXCLUSIVE on
-- runs, each for milliseconds; lock_timeout makes the migration fail cleanly (nothing
-- applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE metrics
    ADD COLUMN software_name text,
    ADD COLUMN software_version text,
    ADD COLUMN run_id uuid,
    ADD CONSTRAINT metrics_run_id_fkey FOREIGN KEY (run_id) REFERENCES runs(id) NOT VALID;
