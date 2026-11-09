-- Serves the model search's compound filter (S1): finds the model files holding a compound without
-- reading every file's records. About 30 bytes per record.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS modeled_compound_records_ccd_id_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS modeled_compound_records_ccd_id_idx
    ON modeled_compound_records (ccd_id, artifact_id);
