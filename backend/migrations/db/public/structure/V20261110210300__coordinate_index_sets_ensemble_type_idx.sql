-- Serves the coordinate-index job's check, every run, for files read as an Ensemble's by its type
-- (conformers 'ensemble_model_type', so no conformer counts) that no model uses as an Ensemble any
-- more (S3): a handful of single-frame Ensemble files among about 225,000 sets.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS coordinate_index_sets_ensemble_type_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS coordinate_index_sets_ensemble_type_idx
    ON coordinate_index_sets (artifact_id)
    WHERE status = 'done' AND conformers = 'ensemble_model_type';
