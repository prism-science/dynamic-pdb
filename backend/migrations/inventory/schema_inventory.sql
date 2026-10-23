-- Read-only inventory of records that break the Dynamic PDB schema v0.2.0 rules added by
-- the C1 migrations. It is a single SELECT: it changes nothing and takes no locks beyond
-- ordinary read snapshots. Run it before validating the NOT VALID constraints and again
-- after each repair round; the repair list is its output.
--
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f schema_inventory.sql              # repair list
--
-- Columns: check_name, object_type, object_id, details (jsonb), check_count (number of
-- records failing that check; NULL when the check cannot run yet). A check with no
-- violations produces no rows. Missing information (an omitted model_type, a NULL sha256)
-- is not a violation: those records stay public.

SELECT findings.check_name,
       findings.object_type,
       findings.object_id,
       findings.details,
       CASE WHEN findings.object_id IS NULL THEN NULL
            ELSE count(*) OVER (PARTITION BY findings.check_name) END AS check_count
FROM (
    SELECT 'metric_unknown_key' AS check_name, 'metric' AS object_type, metrics.id::text AS object_id,
           jsonb_build_object('key', metrics.key,
                              'model_revision_ids', (SELECT jsonb_agg(link.model_revision_id)
                                                     FROM model_revision_metrics link
                                                     WHERE link.metric_id = metrics.id)) AS details
    FROM metrics
    WHERE metrics.key NOT IN ('r_work', 'r_free', 'cc', 'rscc', 'clashscore', 'ramachandran_outliers',
                              'side_chain_outliers', 'molprobity_score', 'wilson_b', 'rmsf')

    UNION ALL
    -- metrics have no producing-software column until Phase D
    SELECT 'metric_missing_software', 'metric', NULL,
           jsonb_build_object('status', 'n/a until Phase D')

    UNION ALL
    SELECT 'metric_duplicate_key', 'model_revision', link.model_revision_id::text,
           jsonb_build_object('key', metrics.key, 'metric_ids', jsonb_agg(metrics.id ORDER BY metrics.created_at, metrics.id))
    FROM model_revision_metrics link
    JOIN metrics ON metrics.id = link.metric_id
    GROUP BY link.model_revision_id, metrics.key
    HAVING count(*) > 1

    UNION ALL
    SELECT 'model_invalid_type', 'model_revision', model_revisions.id::text,
           jsonb_build_object('model_id', model_revisions.model_id, 'model_type', model_revisions.metadata ->> 'model_type')
    FROM model_revisions
    WHERE model_revisions.metadata ->> 'model_type' IS NOT NULL
      AND model_revisions.metadata ->> 'model_type' NOT IN ('Single Conformer', 'Multiconformer', 'Ensemble', 'Deposited')

    UNION ALL
    SELECT 'model_invalid_purpose', 'model_revision', model_revisions.id::text,
           jsonb_build_object('model_id', model_revisions.model_id, 'purpose', model_revisions.metadata ->> 'purpose')
    FROM model_revisions
    WHERE model_revisions.metadata ->> 'purpose' IS NOT NULL
      AND model_revisions.metadata ->> 'purpose' NOT IN ('Model Building', 'Refinement', 'Reference')

    UNION ALL
    SELECT 'run_missing_software_version', 'run', runs.id::text,
           jsonb_build_object('name', runs.name, 'software_name', runs.software_name)
    FROM runs
    WHERE runs.software_version IS NULL OR btrim(runs.software_version) = ''

    UNION ALL
    SELECT 'computed_model_without_run', 'model_revision', model_revisions.id::text,
           jsonb_build_object('model_id', model_revisions.model_id,
                              'model_type', model_revisions.metadata ->> 'model_type',
                              'state', model_revisions.state)
    FROM model_revisions
    WHERE model_revisions.metadata ->> 'model_type' <> 'Deposited'
      AND NOT EXISTS (SELECT 1 FROM model_revision_runs WHERE model_revision_runs.model_revision_id = model_revisions.id)

    UNION ALL
    SELECT 'artifact_malformed_sha256', 'artifact', artifacts.id::text,
           jsonb_build_object('name', artifacts.name, 'sha256', artifacts.sha256)
    FROM artifacts
    WHERE artifacts.sha256 IS NOT NULL AND artifacts.sha256 !~ '^[0-9a-f]{64}$'

    UNION ALL
    SELECT 'artifact_negative_size', 'artifact', artifacts.id::text,
           jsonb_build_object('name', artifacts.name, 'size_bytes', artifacts.size_bytes)
    FROM artifacts
    WHERE artifacts.size_bytes < 0

    UNION ALL
    SELECT 'model_revision_has_composition_lists', 'model_revision', model_revisions.id::text,
           jsonb_build_object('model_id', model_revisions.model_id,
                              'entry_id', models.entry_id,
                              'state', model_revisions.state,
                              'ligands', model_revisions.metadata -> 'ligands',
                              'cofactors', model_revisions.metadata -> 'cofactors')
    FROM model_revisions
    JOIN models ON models.id = model_revisions.model_id
    WHERE model_revisions.metadata ? 'ligands' OR model_revisions.metadata ? 'cofactors'

    UNION ALL
    -- Entries whose active revision still lacks the lists although a model carries them:
    -- the PDB-backed ones wait for the archive re-derivation (C2).
    SELECT 'entry_composition_lists_unresolved', 'entry', entry_revisions.entry_id,
           jsonb_build_object('entry_revision_id', entry_revisions.id,
                              'pdb_id', entry_revisions.metadata #>> '{external_refs,pdb}',
                              'missing_ligands', NOT (entry_revisions.metadata ? 'ligands'),
                              'missing_cofactors', NOT (entry_revisions.metadata ? 'cofactors'))
    FROM entry_revisions
    WHERE entry_revisions.state = 'active'
      AND (NOT (entry_revisions.metadata ? 'ligands') OR NOT (entry_revisions.metadata ? 'cofactors'))
      AND EXISTS (SELECT 1
                  FROM models
                  JOIN model_revisions ON model_revisions.model_id = models.id AND model_revisions.state = 'active'
                  WHERE models.entry_id = entry_revisions.entry_id
                    AND ((model_revisions.metadata ? 'ligands' AND NOT (entry_revisions.metadata ? 'ligands'))
                         OR (model_revisions.metadata ? 'cofactors' AND NOT (entry_revisions.metadata ? 'cofactors'))))

    UNION ALL
    SELECT 'polymer_entity_mutations_unstructured', 'polymer_entity', polymer_entities.id::text,
           jsonb_build_object('mutations', polymer_entities.metadata -> 'mutations',
                              'mutations_raw', polymer_entities.metadata -> 'mutations_raw',
                              'mutations_status', polymer_entities.metadata -> 'mutations_status')
    FROM polymer_entities
    WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'string'
       OR polymer_entities.metadata ->> 'mutations_status' = 'needs_review'
       OR (jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'array'
           AND EXISTS (SELECT 1 FROM jsonb_array_elements(polymer_entities.metadata -> 'mutations') AS element
                       WHERE jsonb_typeof(element) <> 'string' OR element #>> '{}' !~ '^[A-Z][1-9][0-9]*[A-Z]$'))

    UNION ALL
    SELECT 'polymer_entity_uniprot_mapping_invalid', 'polymer_entity', polymer_entities.id::text,
           jsonb_build_object('uniprot_mappings', polymer_entities.metadata -> 'uniprot_mappings')
    FROM polymer_entities
    WHERE jsonb_path_exists(
        polymer_entities.metadata,
        '$.uniprot_mappings[*] ? (
            !exists(@.accession) || @.accession.type() != "string"
            || !exists(@.source) || @.source.type() != "string"
            || !(@.source like_regex "^(sifts|struct_ref|alignment)$")
            || ((@.source == "sifts" || @.source == "alignment")
                && (!exists(@.unp_release) || @.unp_release.type() != "string" || @.unp_release == "")))')

    UNION ALL
    -- Original model lists the ligands/cofactors migration could not merge onto an entry.
    SELECT 'model_revision_composition_lists_unmergeable', 'model_revision', model_revisions.id::text,
           jsonb_build_object('model_id', model_revisions.model_id,
                              'state', model_revisions.state,
                              'ligands', model_revisions.metadata -> 'ligands',
                              'cofactors', model_revisions.metadata -> 'cofactors')
    FROM model_revisions
    WHERE (CASE jsonb_typeof(model_revisions.metadata -> 'ligands')
               WHEN 'array' THEN jsonb_path_exists(model_revisions.metadata, 'strict $.ligands[*] ? (@.type() != "string")')
               WHEN 'null' THEN false
               ELSE jsonb_typeof(model_revisions.metadata -> 'ligands') IS NOT NULL END
           OR CASE jsonb_typeof(model_revisions.metadata -> 'cofactors')
               WHEN 'array' THEN jsonb_path_exists(model_revisions.metadata, 'strict $.cofactors[*] ? (@.type() != "string")')
               WHEN 'null' THEN false
               ELSE jsonb_typeof(model_revisions.metadata -> 'cofactors') IS NOT NULL END)

    UNION ALL
    -- Rows the entry_revisions_composition_lists_chk constraint would reject on UPDATE.
    SELECT 'entry_revision_malformed_composition_lists', 'entry_revision', entry_revisions.id::text,
           jsonb_build_object('entry_id', entry_revisions.entry_id,
                              'ligands', entry_revisions.metadata -> 'ligands',
                              'cofactors', entry_revisions.metadata -> 'cofactors')
    FROM entry_revisions
    WHERE (entry_revisions.metadata ? 'ligands'
           AND (CASE WHEN jsonb_typeof(entry_revisions.metadata -> 'ligands') = 'array'
                     THEN jsonb_path_exists(entry_revisions.metadata, 'strict $.ligands[*] ? (@.type() != "string")')
                     ELSE true END))
       OR (entry_revisions.metadata ? 'cofactors'
           AND (CASE WHEN jsonb_typeof(entry_revisions.metadata -> 'cofactors') = 'array'
                     THEN jsonb_path_exists(entry_revisions.metadata, 'strict $.cofactors[*] ? (@.type() != "string")')
                     ELSE true END))

    UNION ALL
    -- Rows the run_artifacts_direction_chk constraint would reject on UPDATE.
    SELECT 'run_artifact_invalid_direction', 'run', run_artifacts.run_id::text,
           jsonb_build_object('artifact_id', run_artifacts.artifact_id, 'direction', run_artifacts.direction)
    FROM run_artifacts
    WHERE run_artifacts.direction NOT IN ('input', 'output')

    UNION ALL
    -- mutations is neither a string nor an array: not converted by the migration.
    SELECT 'polymer_entity_mutations_unsupported_type', 'polymer_entity', polymer_entities.id::text,
           jsonb_build_object('mutations', polymer_entities.metadata -> 'mutations')
    FROM polymer_entities
    WHERE jsonb_typeof(polymer_entities.metadata -> 'mutations') IN ('number', 'boolean', 'object')

    UNION ALL
    -- mutations array with non-string elements: the Go decoder cannot read it.
    SELECT 'polymer_entity_mutations_array_not_strings', 'polymer_entity', polymer_entities.id::text,
           jsonb_build_object('mutations', polymer_entities.metadata -> 'mutations')
    FROM polymer_entities
    WHERE CASE WHEN jsonb_typeof(polymer_entities.metadata -> 'mutations') = 'array'
               THEN jsonb_path_exists(polymer_entities.metadata, 'strict $.mutations[*] ? (@.type() != "string")')
               ELSE false END
) AS findings
ORDER BY findings.check_name, findings.object_id;
