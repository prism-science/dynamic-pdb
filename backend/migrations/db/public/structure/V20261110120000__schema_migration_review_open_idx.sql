-- Serves the model search's flagged filter: finds the open review flags of one object type without
-- reading the resolved ones (most of the table). About 7,700 open rows on production, under 1 MB.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS schema_migration_review_open_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS schema_migration_review_open_idx
    ON schema_migration_review (object_type, object_id)
    WHERE resolved_at IS NULL;
