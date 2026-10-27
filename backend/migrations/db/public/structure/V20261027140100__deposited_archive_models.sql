-- 120 models in production are the archive's own model: titled "Deposited model", with a PDB
-- reference, in an entry with no other Deposited model. Their active revision still says
-- model_type "Single Conformer" and purpose "Model Building". Archive imports are Deposited with
-- purpose Reference (schema v0.2.0), and the archive sync now writes archive values only onto
-- Deposited models, so without this fix those models would stop syncing.
--
-- Decision D3 (2026-10-27): fix the active revision in place, with no new revision. Older
-- revisions keep their values as history.
--
-- The search index holds each model revision's text, which ends in "<purpose> <model_type>".
-- The same revisions' rows are rewritten, so a search for Deposited finds them.
--
-- The statement updates 120 rows; lock_timeout makes the migration fail cleanly (nothing
-- applied, rerun it) instead of queueing behind a sync transaction that holds one of them.
SET LOCAL lock_timeout = '5s';

WITH mistyped AS (
    SELECT revision.id
    FROM model_revisions revision
    JOIN models model ON model.id = revision.model_id
    WHERE revision.state = 'active'
      AND revision.title = 'Deposited model'
      AND revision.metadata ->> 'model_type' = 'Single Conformer'
      AND revision.metadata ->> 'purpose' = 'Model Building'
      AND revision.metadata -> 'external_refs' ->> 'pdb' IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM models other_model
          JOIN model_revisions other_revision
            ON other_revision.model_id = other_model.id AND other_revision.state = 'active'
          WHERE other_model.entry_id = model.entry_id
            AND other_revision.metadata ->> 'model_type' = 'Deposited'
      )
),
retyped AS (
    UPDATE model_revisions revision
    SET metadata = revision.metadata || '{"model_type": "Deposited", "purpose": "Reference"}'::jsonb,
        updated_at = now()
    FROM mistyped
    WHERE revision.id = mistyped.id
    RETURNING revision.id
)
UPDATE entry_search_index search_row
SET search_text = regexp_replace(search_row.search_text, ' Model Building Single Conformer$', ' Reference Deposited'),
    search_tsv = to_tsvector(
        'simple',
        regexp_replace(search_row.search_text, ' Model Building Single Conformer$', ' Reference Deposited')
    ),
    updated_at = now()
FROM retyped
WHERE search_row.model_type = 'model_revision'
  AND search_row.model_id = retyped.id::text;
