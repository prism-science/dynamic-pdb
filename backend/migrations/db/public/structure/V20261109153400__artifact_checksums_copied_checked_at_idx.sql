-- Serves the coordinate-index job's recent window for archive model files (S2): each run queues the
-- external files the checksum job copied into our bucket since the window's start, by when the copy
-- was recorded, so a copy made long after its artifact was created is still found. Only copied rows
-- are indexed (about 300,000 on prod, about 10 MB).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS artifact_checksums_copied_checked_at_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS artifact_checksums_copied_checked_at_idx
    ON artifact_checksums (checked_at)
    WHERE copy_status = 'copied';
