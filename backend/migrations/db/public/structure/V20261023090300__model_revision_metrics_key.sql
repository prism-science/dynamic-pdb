-- "One metric per key on a model revision" cannot be a plain unique constraint: the key
-- lives on metrics, not on the link table, and existing data may contain duplicates that a
-- migration must not resolve by deleting or choosing a value.
--
-- Plan: model_revision_metrics.metric_key is a trigger-maintained copy of metrics.key.
-- A partial unique index over it (next migration) enforces uniqueness for every row that
-- has a key. Rows that belong to a duplicate group are quarantined: their metric_key stays
-- NULL, so the index skips them, and each group is written to schema_migration_review for
-- repair. After repair, set metric_key = (select key from metrics ...) on those rows.
-- Nothing is deleted or modified in metrics.

ALTER TABLE model_revision_metrics
    ADD COLUMN metric_key text;

CREATE FUNCTION model_revision_metrics_set_metric_key() RETURNS trigger AS $$
BEGIN
    SELECT metrics.key INTO NEW.metric_key FROM metrics WHERE metrics.id = NEW.metric_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER model_revision_metrics_set_metric_key
    BEFORE INSERT OR UPDATE OF metric_id ON model_revision_metrics
    FOR EACH ROW EXECUTE FUNCTION model_revision_metrics_set_metric_key();

CREATE TEMPORARY TABLE duplicate_metric_keys AS
SELECT model_revision_metrics.model_revision_id, metrics.key,
       jsonb_agg(jsonb_build_object('metric_id', metrics.id, 'value', metrics.value)
                 ORDER BY metrics.created_at, metrics.id) AS metrics
FROM model_revision_metrics
JOIN metrics ON metrics.id = model_revision_metrics.metric_id
GROUP BY model_revision_metrics.model_revision_id, metrics.key
HAVING count(*) > 1;

INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'model_revision', model_revision_id::text, 'duplicate_metric_key:' || key,
       jsonb_build_object('key', key, 'metrics', metrics)
FROM duplicate_metric_keys
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

UPDATE model_revision_metrics
SET metric_key = metrics.key
FROM metrics
WHERE metrics.id = model_revision_metrics.metric_id
  AND NOT EXISTS (
      SELECT 1 FROM duplicate_metric_keys
      WHERE duplicate_metric_keys.model_revision_id = model_revision_metrics.model_revision_id
        AND duplicate_metric_keys.key = metrics.key);

DROP TABLE duplicate_metric_keys;
