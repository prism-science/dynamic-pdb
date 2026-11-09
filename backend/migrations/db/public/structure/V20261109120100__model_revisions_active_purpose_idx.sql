-- Serves the model search's purpose filter (S1) without reading metadata, like
-- model_revisions_active_model_type_idx. About 13 MB (measured on a prod copy).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS model_revisions_active_purpose_idx;
--   2. flyway repair
--   3. flyway migrate
--
-- The indexed expression is written exactly as the model search query in
-- backend/internal/db/model_search.go writes it (without the table name); the planner uses the
-- index only then. TestModelSearchIndexExpressions checks the two agree.
CREATE INDEX CONCURRENTLY IF NOT EXISTS model_revisions_active_purpose_idx
    ON model_revisions ((metadata ->> 'purpose'), created_at, id)
    WHERE state = 'active';
