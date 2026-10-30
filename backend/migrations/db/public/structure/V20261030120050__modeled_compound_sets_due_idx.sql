-- Serves the model-compounds job's claim, which takes the oldest due sets in
-- (next_attempt_at, artifact_id) order; finished sets leave the index (T3-M). The table is new
-- and holds a few dozen rows at most when this runs, so the build takes milliseconds.
--
-- One statement per file: the test harnesses send a file as one query, which PostgreSQL runs
-- in a transaction when it holds several statements.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS modeled_compound_sets_due_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS modeled_compound_sets_due_idx
    ON modeled_compound_sets(next_attempt_at, artifact_id)
    WHERE status = 'pending';
