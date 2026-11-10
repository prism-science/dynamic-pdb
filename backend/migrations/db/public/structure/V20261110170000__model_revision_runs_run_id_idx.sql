-- Serves the run_artifacts guard of V20261110170100 (B1b): before a file is linked to a run, it
-- looks up the model revisions that use the run. The primary key (model_revision_id, run_id)
-- cannot answer "revisions of this run". 8.9 MB, built in 0.13 s on the 26 Aug prod copy
-- (371,546 rows).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS model_revision_runs_run_id_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS model_revision_runs_run_id_idx
    ON model_revision_runs (run_id);
