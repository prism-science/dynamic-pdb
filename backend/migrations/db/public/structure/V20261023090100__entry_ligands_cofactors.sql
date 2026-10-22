-- Ligand and cofactor lists belong on the entry revision (sample composition).
--
-- The model revision lists are NOT removed: they stay in model_revisions.metadata until
-- the archive re-derivation job has run, and are copied into schema_migration_review for
-- every entry that was flagged. Nothing is lost.
--
-- Entry revisions are immutable history, but this migration only ADDS keys that were
-- never present ("ligands"/"cofactors" omitted = unknown) to the entry's single active
-- revision. It creates no revision, so no parent/revision_number/author is invented;
-- updated_at is left untouched because no user edited the revision. The at-most-one-active
-- revision index (entry_revisions_active_idx) is not touched.
--
-- Entries with a PDB ID: the archive is the truth, so the entry revision lists stay unset
-- and the entry is flagged for the archive re-derivation job.
-- Entries without a PDB ID: the models' lists are merged (union, order preserved, deduped)
-- onto the active revision and the entry is flagged for review.
--
-- Only lists whose elements are all JSON strings are merged. A value that is not an array
-- (e.g. "ligands": "ATP") or has a non-string element (1, {"a":1}, ["x"], true, null) is
-- never converted: if any active model of an entry holds such a value for a kind, that kind
-- stays unset on the entry revision and the entry is flagged
-- 'ligands_cofactors_not_merged' with the original values. An explicit JSON null means
-- unknown, like an absent key, and is not flagged.

CREATE TEMPORARY TABLE entry_model_lists AS
SELECT
    models.entry_id,
    models.id AS model_id,
    model_revisions.id AS model_revision_id,
    models.created_at AS model_created_at,
    CASE WHEN jsonb_typeof(model_revisions.metadata -> 'ligands') <> 'null'
         THEN model_revisions.metadata -> 'ligands' END AS ligands,
    coalesce(jsonb_typeof(model_revisions.metadata -> 'ligands') = 'array'
             AND NOT jsonb_path_exists(model_revisions.metadata, '$.ligands[*] ? (@.type() != "string")'), false) AS ligands_mergeable,
    CASE WHEN jsonb_typeof(model_revisions.metadata -> 'cofactors') <> 'null'
         THEN model_revisions.metadata -> 'cofactors' END AS cofactors,
    coalesce(jsonb_typeof(model_revisions.metadata -> 'cofactors') = 'array'
             AND NOT jsonb_path_exists(model_revisions.metadata, '$.cofactors[*] ? (@.type() != "string")'), false) AS cofactors_mergeable
FROM model_revisions
JOIN models ON models.id = model_revisions.model_id
WHERE model_revisions.state = 'active'
  AND (jsonb_typeof(model_revisions.metadata -> 'ligands') <> 'null'
       OR jsonb_typeof(model_revisions.metadata -> 'cofactors') <> 'null');

CREATE TEMPORARY TABLE entry_list_targets AS
SELECT
    entry_revisions.entry_id,
    entry_revisions.id AS entry_revision_id,
    entry_revisions.metadata AS entry_metadata,
    coalesce(entry_revisions.metadata #>> '{external_refs,pdb}', '') <> '' AS has_pdb_id
FROM entry_revisions
WHERE entry_revisions.state = 'active'
  AND entry_revisions.entry_id IN (SELECT entry_id FROM entry_model_lists);

CREATE TEMPORARY TABLE entry_merged_lists AS
SELECT
    entry_model_lists.entry_id,
    (SELECT jsonb_agg(first_seen.value ORDER BY first_seen.position)
       FROM (SELECT elements.value, min(models_ordered.rank * 100000 + elements.ordinality) AS position
               FROM (SELECT mine.model_id, row_number() OVER (ORDER BY mine.model_created_at, mine.model_id) AS rank,
                            mine.ligands
                       FROM entry_model_lists mine
                      WHERE mine.entry_id = entry_model_lists.entry_id AND mine.ligands_mergeable) models_ordered,
                    jsonb_array_elements_text(models_ordered.ligands) WITH ORDINALITY AS elements(value, ordinality)
              GROUP BY elements.value) first_seen) AS ligands,
    bool_or(entry_model_lists.ligands_mergeable) AS ligands_known,
    bool_or(entry_model_lists.ligands IS NOT NULL AND NOT entry_model_lists.ligands_mergeable) AS ligands_blocked,
    (SELECT jsonb_agg(first_seen.value ORDER BY first_seen.position)
       FROM (SELECT elements.value, min(models_ordered.rank * 100000 + elements.ordinality) AS position
               FROM (SELECT mine.model_id, row_number() OVER (ORDER BY mine.model_created_at, mine.model_id) AS rank,
                            mine.cofactors
                       FROM entry_model_lists mine
                      WHERE mine.entry_id = entry_model_lists.entry_id AND mine.cofactors_mergeable) models_ordered,
                    jsonb_array_elements_text(models_ordered.cofactors) WITH ORDINALITY AS elements(value, ordinality)
              GROUP BY elements.value) first_seen) AS cofactors,
    bool_or(entry_model_lists.cofactors_mergeable) AS cofactors_known,
    bool_or(entry_model_lists.cofactors IS NOT NULL AND NOT entry_model_lists.cofactors_mergeable) AS cofactors_blocked,
    jsonb_agg(jsonb_build_object(
        'model_id', entry_model_lists.model_id,
        'model_revision_id', entry_model_lists.model_revision_id,
        'ligands', entry_model_lists.ligands,
        'cofactors', entry_model_lists.cofactors
    ) ORDER BY entry_model_lists.model_created_at, entry_model_lists.model_id) AS model_lists
FROM entry_model_lists
GROUP BY entry_model_lists.entry_id;

-- A kind is merged only when no model of the entry holds an unmergeable value for it, so the
-- entry never claims a partial list.
CREATE TEMPORARY TABLE entry_list_decisions AS
SELECT
    entry_merged_lists.*,
    entry_merged_lists.ligands_known AND NOT entry_merged_lists.ligands_blocked AS merge_ligands,
    entry_merged_lists.cofactors_known AND NOT entry_merged_lists.cofactors_blocked AS merge_cofactors
FROM entry_merged_lists;

INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'entry', entry_list_targets.entry_id,
       'ligands_cofactors_rederive_from_archive',
       jsonb_build_object('model_lists', entry_list_decisions.model_lists)
FROM entry_list_targets
JOIN entry_list_decisions USING (entry_id)
WHERE entry_list_targets.has_pdb_id
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'entry', entry_list_decisions.entry_id,
       'ligands_cofactors_no_active_entry_revision',
       jsonb_build_object('model_lists', entry_list_decisions.model_lists)
FROM entry_list_decisions
WHERE entry_list_decisions.entry_id NOT IN (SELECT entry_id FROM entry_list_targets)
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'entry', entry_list_targets.entry_id,
       'ligands_cofactors_not_merged',
       jsonb_build_object(
           'entry_revision_id', entry_list_targets.entry_revision_id,
           'ligands_not_merged', entry_list_decisions.ligands_blocked,
           'cofactors_not_merged', entry_list_decisions.cofactors_blocked,
           'model_lists', entry_list_decisions.model_lists)
FROM entry_list_targets
JOIN entry_list_decisions USING (entry_id)
WHERE NOT entry_list_targets.has_pdb_id
  AND (entry_list_decisions.ligands_blocked OR entry_list_decisions.cofactors_blocked)
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

INSERT INTO schema_migration_review (object_type, object_id, issue, details)
SELECT 'entry', entry_list_targets.entry_id,
       'ligands_cofactors_merged_from_models',
       jsonb_build_object(
           'entry_revision_id', entry_list_targets.entry_revision_id,
           'merged_ligands', CASE WHEN entry_list_decisions.merge_ligands AND NOT (entry_list_targets.entry_metadata ? 'ligands')
                                  THEN coalesce(entry_list_decisions.ligands, '[]'::jsonb) END,
           'merged_cofactors', CASE WHEN entry_list_decisions.merge_cofactors AND NOT (entry_list_targets.entry_metadata ? 'cofactors')
                                    THEN coalesce(entry_list_decisions.cofactors, '[]'::jsonb) END,
           'model_lists', entry_list_decisions.model_lists)
FROM entry_list_targets
JOIN entry_list_decisions USING (entry_id)
WHERE NOT entry_list_targets.has_pdb_id
ON CONFLICT (object_type, object_id, issue) DO NOTHING;

UPDATE entry_revisions
SET metadata = entry_revisions.metadata
    || CASE WHEN entry_list_decisions.merge_ligands AND NOT (entry_revisions.metadata ? 'ligands')
            THEN jsonb_build_object('ligands', coalesce(entry_list_decisions.ligands, '[]'::jsonb))
            ELSE '{}'::jsonb END
    || CASE WHEN entry_list_decisions.merge_cofactors AND NOT (entry_revisions.metadata ? 'cofactors')
            THEN jsonb_build_object('cofactors', coalesce(entry_list_decisions.cofactors, '[]'::jsonb))
            ELSE '{}'::jsonb END
FROM entry_list_targets
JOIN entry_list_decisions USING (entry_id)
WHERE entry_revisions.id = entry_list_targets.entry_revision_id
  AND NOT entry_list_targets.has_pdb_id;

DROP TABLE entry_list_decisions;
DROP TABLE entry_merged_lists;
DROP TABLE entry_list_targets;
DROP TABLE entry_model_lists;
