-- Serves the model search's organism filter (S1). The list is lowercased so a scientific name
-- matches ignoring case; NCBI taxonomy IDs are numbers and unchanged. About 2 MB for 241k polymer
-- entities (measured on a prod copy).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS polymer_entities_source_organisms_idx;
--   2. flyway repair
--   3. flyway migrate
--
-- The indexed expression is written exactly as the model search query in
-- backend/internal/db/model_search.go writes it (without the table name); the planner uses the
-- index only then. TestModelSearchIndexExpressions checks the two agree.
CREATE INDEX CONCURRENTLY IF NOT EXISTS polymer_entities_source_organisms_idx
    ON polymer_entities USING gin ((cast(lower(cast(metadata -> 'source_organisms' as text)) as jsonb)) jsonb_path_ops);
