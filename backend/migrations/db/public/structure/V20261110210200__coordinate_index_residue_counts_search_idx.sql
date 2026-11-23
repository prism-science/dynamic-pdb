-- Serves the residue filter (S4): finds the model files with residues of a type at or above a
-- conformer count, and adds up their residues from the index alone (INCLUDE). The S3 spike measured
-- 1-140 ms for example search 1 with this index on all models. The table is new and empty when
-- this runs, so the build takes milliseconds.
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS coordinate_index_residue_counts_search_idx;
--   2. flyway repair
--   3. flyway migrate
CREATE INDEX CONCURRENTLY IF NOT EXISTS coordinate_index_residue_counts_search_idx
    ON coordinate_index_residue_counts (residue_type, conformer_count, artifact_id)
    INCLUDE (residues);
