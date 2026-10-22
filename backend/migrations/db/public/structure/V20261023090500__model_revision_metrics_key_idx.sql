-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. It waits for
-- older transactions to finish, so no lock_timeout is set here. If a duplicate metric was
-- attached between the backfill and this index, the build fails and leaves an INVALID index:
-- run `DROP INDEX CONCURRENTLY IF EXISTS model_revision_metrics_revision_key_idx;`, rerun the
-- backfill (090400, idempotent) and `flyway repair`, then migrate again.
CREATE UNIQUE INDEX CONCURRENTLY model_revision_metrics_revision_key_idx
    ON model_revision_metrics(model_revision_id, metric_key)
    WHERE metric_key IS NOT NULL;
