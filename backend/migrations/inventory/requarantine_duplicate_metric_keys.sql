-- Re-quarantines model revisions that hold two or more metrics with the same key.
--
-- Run it when V20261023090500 (unique index on model_revision_metrics(model_revision_id,
-- metric_key)) failed with "could not create unique index ... Key (...) is duplicated", and
-- at any time later to find duplicates that were attached after the backfill (V20261023090400
-- only handles rows whose metric_key is still NULL; the trigger gives every later link its real
-- key, so a late duplicate keeps it).
--
-- Idempotent: every statement is a no-op when no duplicates remain, so it is safe to rerun.
-- Nothing is deleted and no metric is chosen: every link of a duplicated group gets
-- metric_key = '<key>#duplicate#<metric_id>' (unique, so the index accepts it, and the API
-- still returns the real key because it reads metrics.key), exactly like 090400 does.
-- Quarantined revisions are listed in schema_migration_review as 'duplicate_metric_key:<key>'.
--
-- Takes only row locks (ROW EXCLUSIVE) on the duplicated links; reads and writes continue.
-- The first statement prints what it quarantined: zero rows means nothing was left.
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/migrations/inventory/requarantine_duplicate_metric_keys.sql

UPDATE model_revision_metrics
SET metric_key = model_revision_metrics.metric_key || '#duplicate#' || model_revision_metrics.metric_id::text
FROM (SELECT model_revision_id, metric_key
      FROM model_revision_metrics
      WHERE metric_key IS NOT NULL
        AND metric_key NOT LIKE '%#duplicate#%'
      GROUP BY model_revision_id, metric_key
      HAVING count(*) > 1) duplicated
WHERE model_revision_metrics.model_revision_id = duplicated.model_revision_id
  AND model_revision_metrics.metric_key = duplicated.metric_key
RETURNING model_revision_metrics.model_revision_id, model_revision_metrics.metric_id, model_revision_metrics.metric_key;

INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'model_revision', quarantined.model_revision_id::text, 'duplicate_metric_key:' || quarantined.key,
       jsonb_build_object('key', quarantined.key,
                          'metrics', (SELECT jsonb_agg(jsonb_build_object('metric_id', metrics.id, 'value', metrics.value)
                                                       ORDER BY metrics.created_at, metrics.id)
                                      FROM model_revision_metrics link
                                      JOIN metrics ON metrics.id = link.metric_id
                                      WHERE link.model_revision_id = quarantined.model_revision_id
                                        AND metrics.key = quarantined.key))
FROM (SELECT DISTINCT link.model_revision_id, metrics.key
      FROM model_revision_metrics link
      JOIN metrics ON metrics.id = link.metric_id
      WHERE link.metric_key LIKE '%#duplicate#%') quarantined
ON CONFLICT (object_type, object_id, issue) DO NOTHING;
