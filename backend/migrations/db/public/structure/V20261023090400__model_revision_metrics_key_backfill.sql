-- Backfills model_revision_metrics.metric_key in committed batches of 5000 rows, so no
-- single transaction holds row locks or rewrites the table for long. It takes no table-level
-- lock beyond ROW EXCLUSIVE, so reads and writes continue. Runs outside a transaction
-- (executeInTransaction=false) because it commits between batches; it is idempotent and safe
-- to rerun after an interruption (only rows with a NULL metric_key are touched).
--
-- A link that shares (model_revision_id, metrics.key) with another link of the same revision
-- is quarantined: its metric_key becomes '<key>#duplicate#<metric_id>', which is unique, so
-- the unique index (next migrations) accepts it, nothing is deleted or chosen, and the
-- group is written to schema_migration_review for repair. Repair SQL is in
-- backend/migrations/inventory/README.md.
DO $migration$
DECLARE
    batch_size CONSTANT integer := 5000;
    updated_rows integer;
BEGIN
    LOOP
        WITH batch AS (
            SELECT link.model_revision_id, link.metric_id, metrics.key,
                   EXISTS (SELECT 1
                           FROM model_revision_metrics sibling
                           JOIN metrics sibling_metric ON sibling_metric.id = sibling.metric_id
                           WHERE sibling.model_revision_id = link.model_revision_id
                             AND sibling_metric.key = metrics.key
                             AND sibling.metric_id <> link.metric_id) AS duplicated
            FROM model_revision_metrics link
            JOIN metrics ON metrics.id = link.metric_id
            WHERE link.metric_key IS NULL
            ORDER BY link.model_revision_id, link.metric_id
            LIMIT batch_size
            FOR UPDATE OF link
        )
        UPDATE model_revision_metrics
        SET metric_key = CASE WHEN batch.duplicated
                              THEN batch.key || '#duplicate#' || batch.metric_id::text
                              ELSE batch.key END
        FROM batch
        WHERE model_revision_metrics.model_revision_id = batch.model_revision_id
          AND model_revision_metrics.metric_id = batch.metric_id;
        GET DIAGNOSTICS updated_rows = ROW_COUNT;
        COMMIT;
        EXIT WHEN updated_rows = 0;
    END LOOP;

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
    COMMIT;
END
$migration$;
