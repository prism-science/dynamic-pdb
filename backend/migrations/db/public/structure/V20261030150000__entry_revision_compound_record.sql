-- Entry revisions keep the decision record of their composition lists, as the mmCIF extraction
-- returns it (T3-B2): compound_sources holds the groups ligands, cofactors and excluded, each
-- mapping a CCD ID to {rule, list_version, review_reason}, and glycan_descriptors lists one
-- glycan per branched entity with its WURCS descriptors. review_reason is present only when the
-- parser flagged the compound for review.
--
-- Existing revisions keep NULL: their lists were not derived by the current rules. The archive
-- re-derivation job (T1-C2) fills both for archive entries, and the entry sync keeps them current
-- when it applies a new archive version.
--
-- Both columns are nullable with no default, so ADD COLUMN changes only the catalog. It takes
-- ACCESS EXCLUSIVE on entry_revisions for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE entry_revisions ADD COLUMN compound_sources jsonb;
ALTER TABLE entry_revisions ADD COLUMN glycan_descriptors jsonb;
