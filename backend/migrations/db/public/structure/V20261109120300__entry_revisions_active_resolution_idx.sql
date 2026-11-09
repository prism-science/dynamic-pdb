-- Serves the model search's resolution range (S1). A missing or non-numeric resolution indexes as
-- null and never matches. About 2 MB for 73k active entries (estimate).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS entry_revisions_active_resolution_idx;
--   2. flyway repair
--   3. flyway migrate
--
-- The indexed expression is written exactly as the model search query in
-- backend/internal/db/model_search.go writes it (without the table name); the planner uses the
-- index only then. TestModelSearchIndexExpressions checks the two agree.
CREATE INDEX CONCURRENTLY IF NOT EXISTS entry_revisions_active_resolution_idx
    ON entry_revisions ((case when jsonb_typeof(metadata -> 'resolution') = 'number'
                              then cast(metadata ->> 'resolution' as numeric) end))
    WHERE state = 'active';
