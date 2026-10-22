-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. It cannot run in a transaction
-- (executeInTransaction=false), so a failure or interruption is not rolled back: Flyway records
-- the migration as failed and may leave an INVALID index.
--
-- If a duplicate metric was attached between the backfill and this index, the build fails with
-- "could not create unique index". That duplicate already holds its real key (the trigger
-- filled it), so rerunning 090400 would not touch it. Recover with:
--   1. DROP INDEX CONCURRENTLY IF EXISTS model_revision_metrics_revision_key_idx;
--   2. psql -v ON_ERROR_STOP=1 -f backend/migrations/inventory/requarantine_duplicate_metric_keys.sql
--      (idempotent, prints the links it quarantines; nothing is deleted)
--   3. flyway repair   (removes the failed history row; also needed after an interruption)
--   4. flyway migrate
CREATE UNIQUE INDEX CONCURRENTLY model_revision_metrics_revision_key_idx
    ON model_revision_metrics(model_revision_id, metric_key)
    WHERE metric_key IS NOT NULL;
