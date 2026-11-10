-- Serves the lookups of the model revisions that use a model file: the coordinate-index job's
-- per-file read of the entry's FASTA records (S2), and the model-compounds job's re-indexing of
-- the revisions with a file, which scan model_revisions without it. About 12 MB for the ~370,000
-- revisions on prod; the build reads the table once (about 150 MB), a few seconds.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS model_revisions_primary_artifact_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS model_revisions_primary_artifact_idx
    ON model_revisions (primary_artifact_id);
