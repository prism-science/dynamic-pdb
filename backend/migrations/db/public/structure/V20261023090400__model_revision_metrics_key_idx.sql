CREATE UNIQUE INDEX CONCURRENTLY model_revision_metrics_revision_key_idx
    ON model_revision_metrics(model_revision_id, metric_key)
    WHERE metric_key IS NOT NULL;
