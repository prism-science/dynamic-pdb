-- T1-E: the database refuses a UniProt mapping without a string accession or a known source
-- (sifts, struct_ref, alignment), and a sifts or alignment mapping without its UniProt release.
-- Same rule as validation.PolymerEntity. The data sync used to store a SIFTS mapping without a
-- release when the release lookup failed; in enforce mode it now skips that RCSB version and
-- reports it (sync_validation_failed) instead of writing it.
--
-- NOT VALID: no scan (V20261104150900 validates the existing rows). A NOT VALID check still
-- applies to an UPDATE of an existing row, so the inventory check
-- 'polymer_entity_uniprot_mapping_invalid' must be empty before this ships (prod: 0 rows on
-- 2026-11-04).
--
-- ACCESS EXCLUSIVE on polymer_entities for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

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
