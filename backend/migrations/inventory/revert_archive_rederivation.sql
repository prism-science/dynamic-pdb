-- Reverts what the archive re-derivation job (T1-C2) changed, from the before values it logged in
-- archive_rederivations. Run it with the job switched off:
--
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/migrations/inventory/revert_archive_rederivation.sql
--
-- A revision is reverted only as a whole, and only while it is still active and every value the
-- job changed (its lists, decision record and glycans, and the mutations of its polymer entities)
-- still holds what the job wrote. Anything changed since is kept, and the revision is listed as
-- skipped. For each reverted revision the script also reopens the review rows the job resolved and
-- deletes the log row, so a later run of the job would derive it again.
--
-- Dry-run rows changed nothing and are left alone. The script prints the number of reverted
-- revisions, then the skipped ones.
BEGIN;

CREATE TEMP TABLE revert_logged ON COMMIT DROP AS
SELECT entry_revision_id, entry_id, before, after
FROM archive_rederivations
WHERE NOT dry_run AND before IS NOT NULL;

-- The compounds of a revision as the job left them; JSON null in the log is an unset value.
CREATE TEMP TABLE revert_compounds ON COMMIT DROP AS
SELECT logged.entry_revision_id,
       logged.before -> 'compounds' AS before,
       revision.state = 'active'
       AND revision.metadata -> 'ligands' IS NOT DISTINCT FROM nullif(logged.after -> 'compounds' -> 'ligands', 'null')
       AND revision.metadata -> 'cofactors' IS NOT DISTINCT FROM nullif(logged.after -> 'compounds' -> 'cofactors', 'null')
       AND revision.compound_sources IS NOT DISTINCT FROM nullif(logged.after -> 'compounds' -> 'compound_sources', 'null')
       AND revision.glycan_descriptors IS NOT DISTINCT FROM nullif(logged.after -> 'compounds' -> 'glycan_descriptors', 'null')
         AS unchanged_since
FROM revert_logged logged
JOIN entry_revisions revision ON revision.id = logged.entry_revision_id
WHERE logged.before ? 'compounds';

-- The polymer entities whose mutations the job changed, paired by position in before and after.
CREATE TEMP TABLE revert_entities ON COMMIT DROP AS
SELECT logged.entry_revision_id,
       entity.id AS polymer_entity_id,
       before_entity.value AS before,
       entity.metadata -> 'mutations' IS NOT DISTINCT FROM nullif(after_entity.value -> 'mutations', 'null')
       AND entity.metadata -> 'mutations_raw' IS NOT DISTINCT FROM nullif(after_entity.value -> 'mutations_raw', 'null')
       AND entity.metadata -> 'mutations_status' IS NOT DISTINCT FROM nullif(after_entity.value -> 'mutations_status', 'null')
         AS unchanged_since
FROM revert_logged logged
CROSS JOIN LATERAL jsonb_array_elements(coalesce(logged.before -> 'polymer_entities', '[]')) WITH ORDINALITY
  AS before_entity(value, position)
JOIN LATERAL jsonb_array_elements(coalesce(logged.after -> 'polymer_entities', '[]')) WITH ORDINALITY
  AS after_entity(value, position) ON after_entity.position = before_entity.position
JOIN polymer_entities entity ON entity.id = (before_entity.value ->> 'id')::uuid;

CREATE TEMP TABLE revert_decisions ON COMMIT DROP AS
SELECT logged.entry_revision_id,
       logged.entry_id,
       coalesce(revision.state = 'active', false)
       AND NOT EXISTS (SELECT 1 FROM revert_compounds c
                       WHERE c.entry_revision_id = logged.entry_revision_id AND NOT c.unchanged_since)
       AND NOT EXISTS (SELECT 1 FROM revert_entities e
                       WHERE e.entry_revision_id = logged.entry_revision_id AND NOT e.unchanged_since)
         AS revert,
       CASE
         WHEN revision.state IS DISTINCT FROM 'active' THEN 'revision is no longer active'
         ELSE 'a value the job changed was changed again since'
       END AS skip_reason
FROM revert_logged logged
LEFT JOIN entry_revisions revision ON revision.id = logged.entry_revision_id;

UPDATE entry_revisions revision
SET metadata = (revision.metadata - 'ligands' - 'cofactors')
               || jsonb_strip_nulls(jsonb_build_object(
                    'ligands', compounds.before -> 'ligands',
                    'cofactors', compounds.before -> 'cofactors')),
    compound_sources = nullif(compounds.before -> 'compound_sources', 'null'),
    glycan_descriptors = nullif(compounds.before -> 'glycan_descriptors', 'null')
FROM revert_compounds compounds
JOIN revert_decisions decision USING (entry_revision_id)
WHERE revision.id = compounds.entry_revision_id AND decision.revert;

UPDATE polymer_entities entity
SET metadata = (entity.metadata - 'mutations' - 'mutations_raw' - 'mutations_status')
               || jsonb_strip_nulls(jsonb_build_object(
                    'mutations', entities.before -> 'mutations',
                    'mutations_raw', entities.before -> 'mutations_raw',
                    'mutations_status', entities.before -> 'mutations_status'))
FROM revert_entities entities
JOIN revert_decisions decision USING (entry_revision_id)
WHERE entity.id = entities.polymer_entity_id AND decision.revert;

UPDATE schema_migration_review review
SET resolved_at = NULL, resolved_by = NULL
FROM revert_decisions decision
WHERE decision.revert
  AND review.resolved_by = 'archive-rederive-job'
  AND ((review.object_type = 'entry' AND review.object_id = decision.entry_id)
       OR (review.object_type = 'polymer_entity'
           AND review.object_id IN (SELECT polymer_entity_id::text FROM revert_entities e
                                    WHERE e.entry_revision_id = decision.entry_revision_id)));

DELETE FROM archive_rederivations logged
USING revert_decisions decision
WHERE logged.entry_revision_id = decision.entry_revision_id AND decision.revert;

SELECT count(*) AS reverted_revisions FROM revert_decisions WHERE revert;

SELECT entry_revision_id, entry_id, skip_reason
FROM revert_decisions
WHERE NOT revert
ORDER BY entry_id;

COMMIT;
