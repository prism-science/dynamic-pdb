-- Read-only invariants for "no data lost" checks: run before and after the C1 migrations and
-- diff the two outputs. Everything below must be identical (the migrations add columns and
-- review rows, they never delete rows or drop model-level lists). Mutation values are not in the
-- list on purpose: they are restructured by design (originals are kept in mutations_raw).
--
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -At -f backend/migrations/inventory/counts.sql > before.txt
SELECT name, value FROM (
    SELECT 'entries' AS name, count(*)::text AS value FROM entries
    UNION ALL SELECT 'entry_revisions', count(*)::text FROM entry_revisions
    UNION ALL SELECT 'models', count(*)::text FROM models
    UNION ALL SELECT 'model_revisions', count(*)::text FROM model_revisions
    UNION ALL SELECT 'artifacts', count(*)::text FROM artifacts
    UNION ALL SELECT 'metrics', count(*)::text FROM metrics
    UNION ALL SELECT 'model_revision_metrics', count(*)::text FROM model_revision_metrics
    UNION ALL SELECT 'runs', count(*)::text FROM runs
    UNION ALL SELECT 'run_artifacts', count(*)::text FROM run_artifacts
    UNION ALL SELECT 'polymer_entities', count(*)::text FROM polymer_entities
    UNION ALL SELECT 'model_revisions_with_ligands_key', count(*)::text FROM model_revisions WHERE metadata ? 'ligands'
    UNION ALL SELECT 'model_revisions_with_cofactors_key', count(*)::text FROM model_revisions WHERE metadata ? 'cofactors'
    UNION ALL SELECT 'model_lists_md5', md5(coalesce(string_agg(id::text || coalesce(metadata ->> 'ligands', '-') || coalesce(metadata ->> 'cofactors', '-'), ',' ORDER BY id), ''))
                                        FROM model_revisions
    UNION ALL SELECT 'metrics_md5', md5(coalesce(string_agg(id::text || key || value::text, ',' ORDER BY id), '')) FROM metrics
    UNION ALL SELECT 'model_revision_metric_links_md5', md5(coalesce(string_agg(model_revision_id::text || metric_id::text, ',' ORDER BY model_revision_id, metric_id), ''))
                                        FROM model_revision_metrics
    UNION ALL SELECT 'run_artifact_links_md5', md5(coalesce(string_agg(run_id::text || artifact_id::text || direction, ',' ORDER BY run_id, artifact_id, direction), ''))
                                        FROM run_artifacts
    UNION ALL SELECT 'archived_entry_revisions_md5', md5(coalesce(string_agg(id::text || metadata::text, ',' ORDER BY id), ''))
                                        FROM entry_revisions WHERE state <> 'active'
) invariants
ORDER BY name;
