-- Serves the model search's source filter (S1): pdb_archive is models.ModelMetadata.IsArchiveModel,
-- origin archive or model type Deposited. About 10 MB (estimate).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS model_revisions_active_archive_idx;
--   2. flyway repair
--   3. flyway migrate
--
-- The indexed expression is written exactly as the model search query in
-- backend/internal/db/model_search.go writes it (without the table name); the planner uses the
-- index only then. TestModelSearchIndexExpressions checks the two agree.
CREATE INDEX CONCURRENTLY IF NOT EXISTS model_revisions_active_archive_idx
    ON model_revisions ((coalesce(metadata ->> 'origin', '') = 'archive'
                         or coalesce(metadata ->> 'model_type', '') = 'Deposited'), created_at, id)
    WHERE state = 'active';
