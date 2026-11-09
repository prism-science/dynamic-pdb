-- Serves the model search's model_type filter (GET /v1/models?model_type=…, S1) without reading
-- metadata: archive models keep residue_data in metadata, so reading it means unpacking ~10 kB per
-- row. created_at, id keep one type's models in list order. About 13 MB for 225k active models
-- (measured on a prod copy).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS model_revisions_active_model_type_idx;
--   2. flyway repair
--   3. flyway migrate
--
-- The indexed expression is written exactly as the model search query in
-- backend/internal/db/model_search.go writes it (without the table name); the planner uses the
-- index only then. TestModelSearchIndexExpressions checks the two agree.
CREATE INDEX CONCURRENTLY IF NOT EXISTS model_revisions_active_model_type_idx
    ON model_revisions ((metadata ->> 'model_type'), created_at, id)
    WHERE state = 'active';
