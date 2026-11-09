-- Serves the model search's ligand_or_cofactor filter (S1) on the curated Ligands list, with
-- containment (@>). The list is lowercased, as people may give IDs in any case, so a search
-- matches ignoring case. A few MB for 73k active entries (estimate).
--
-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS entry_revisions_active_ligands_idx;
--   2. flyway repair
--   3. flyway migrate
--
-- The indexed expression is written exactly as the model search query in
-- backend/internal/db/model_search.go writes it (without the table name); the planner uses the
-- index only then. TestModelSearchIndexExpressions checks the two agree.
CREATE INDEX CONCURRENTLY IF NOT EXISTS entry_revisions_active_ligands_idx
    ON entry_revisions USING gin ((cast(lower(cast(metadata -> 'ligands' as text)) as jsonb)) jsonb_path_ops)
    WHERE state = 'active';
