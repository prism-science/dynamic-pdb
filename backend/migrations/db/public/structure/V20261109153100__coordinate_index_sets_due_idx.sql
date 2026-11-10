-- Serves the coordinate-index job's claim, which takes uploads before fill rows, each in
-- (next_attempt_at, artifact_id) order, and its check for pending fill rows; finished sets leave
-- the index (S2). The table is new and empty when this runs, so the build takes milliseconds.
--
-- One statement per file: the test harnesses send a file as one query, which PostgreSQL runs
-- in a transaction when it holds several statements.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS coordinate_index_sets_due_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS coordinate_index_sets_due_idx
    ON coordinate_index_sets (fill, next_attempt_at, artifact_id)
    WHERE status = 'pending';
