-- Serves the model search's chain filter (S2): finds the model files with protein chains in a
-- length range without reading every chain row. The table is new and empty when this runs.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS coordinate_index_chains_protein_length_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS coordinate_index_chains_protein_length_idx
    ON coordinate_index_chains (deposited_length, artifact_id)
    WHERE protein;
