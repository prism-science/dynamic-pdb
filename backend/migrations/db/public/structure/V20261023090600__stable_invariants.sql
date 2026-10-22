-- Stable invariants, added NOT VALID: they are enforced for every new or updated row
-- immediately, while existing violators stay (and stay public) until the repair list from
-- backend/migrations/inventory/schema_inventory.sql is worked off. Each constraint is then
-- validated with ALTER TABLE ... VALIDATE CONSTRAINT (SHARE UPDATE EXCLUSIVE, no write block).
-- ADD CONSTRAINT ... NOT VALID needs only a brief ACCESS EXCLUSIVE lock and scans nothing.

ALTER TABLE metrics
    ADD CONSTRAINT metrics_key_known_chk CHECK (key IN (
        'r_work', 'r_free', 'cc', 'rscc', 'clashscore', 'ramachandran_outliers',
        'side_chain_outliers', 'molprobity_score', 'wilson_b', 'rmsf')) NOT VALID;

ALTER TABLE model_revisions
    ADD CONSTRAINT model_revisions_model_type_chk CHECK (
        metadata ->> 'model_type' IS NULL
        OR metadata ->> 'model_type' IN ('Single Conformer', 'Multiconformer', 'Ensemble', 'Deposited')) NOT VALID,
    ADD CONSTRAINT model_revisions_purpose_chk CHECK (
        metadata ->> 'purpose' IS NULL
        OR metadata ->> 'purpose' IN ('Model Building', 'Refinement', 'Reference')) NOT VALID;

ALTER TABLE entry_revisions
    ADD CONSTRAINT entry_revisions_composition_lists_chk CHECK (
        (metadata -> 'ligands' IS NULL OR jsonb_typeof(metadata -> 'ligands') = 'array')
        AND (metadata -> 'cofactors' IS NULL OR jsonb_typeof(metadata -> 'cofactors') = 'array')) NOT VALID;

ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_sha256_format_chk CHECK (sha256 IS NULL OR sha256 ~ '^[0-9a-f]{64}$') NOT VALID,
    ADD CONSTRAINT artifacts_size_bytes_chk CHECK (size_bytes IS NULL OR size_bytes >= 0) NOT VALID;

-- Sources: sifts, struct_ref, alignment. sifts and alignment need unp_release.
ALTER TABLE polymer_entities
    ADD CONSTRAINT polymer_entities_uniprot_mappings_chk CHECK (NOT jsonb_path_exists(
        metadata,
        '$.uniprot_mappings[*] ? (
            !exists(@.accession) || @.accession.type() != "string"
            || !exists(@.source) || @.source.type() != "string"
            || !(@.source like_regex "^(sifts|struct_ref|alignment)$")
            || ((@.source == "sifts" || @.source == "alignment")
                && (!exists(@.unp_release) || @.unp_release.type() != "string" || @.unp_release == "")))'
    )) NOT VALID;

-- run_artifacts: one artifact may be both input and output of a run. The unique index was
-- built concurrently in the previous migration; swapping the primary key onto it is
-- metadata-only. position is the artifact's order within the run, unknown when NULL.
ALTER TABLE run_artifacts
    DROP CONSTRAINT run_artifacts_pkey,
    ADD CONSTRAINT run_artifacts_pkey PRIMARY KEY USING INDEX run_artifacts_run_artifact_direction_idx,
    ADD COLUMN position integer,
    ADD CONSTRAINT run_artifacts_direction_chk CHECK (direction IN ('input', 'output')) NOT VALID,
    ADD CONSTRAINT run_artifacts_position_chk CHECK (position IS NULL OR position >= 0) NOT VALID;
