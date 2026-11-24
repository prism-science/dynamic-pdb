-- coordinate_index_sets.residue_key may also be 'mapped' (S3, researchers' answer of 17 Nov, Q2):
-- residues a file writes without label numbering (PDB format) were given it through the entry's
-- deposited mmCIF (_pdbx_poly_seq_scheme), so every residue is keyed by label numbering. 'author'
-- and 'mixed' stay for residues no mapping covered.
--
-- The check is replaced by the same check with 'mapped' added, NOT VALID (no scan; it applies to
-- every insert and update), and validated by V20261118120100. Until the conformer counts are
-- switched on, every row has the conformer columns null, which both checks allow. ACCESS EXCLUSIVE
-- on coordinate_index_sets for milliseconds; lock_timeout makes the migration fail cleanly
-- (nothing applied, rerun it) instead of queueing behind a long transaction.
--
-- Like the check it replaces (V20261110210000), the branches sit inside coalesce(..., false): a CHECK
-- passes when its expression is NULL, so a NULL in a column a branch needs would otherwise let the row
-- through.
SET LOCAL lock_timeout = '5s';

ALTER TABLE coordinate_index_sets
    DROP CONSTRAINT coordinate_index_sets_conformers_check,
    ADD CONSTRAINT coordinate_index_sets_conformers_check CHECK (coalesce(
        (residues_version IS NULL AND conformers IS NULL AND residue_key IS NULL AND residues IS NULL
            AND multi_conformer_residues IS NULL AND read_as_ensemble_type IS NULL)
        OR (residues_version IS NOT NULL AND conformers = 'counted' AND read_as_ensemble_type = false
            AND residues >= 0 AND multi_conformer_residues BETWEEN 0 AND residues
            AND (residue_key IS NULL) = (residues = 0)
            AND (residue_key IS NULL OR residue_key IN ('label', 'mapped', 'author', 'mixed')))
        OR (residues_version IS NOT NULL AND conformers = 'ensemble_model_frames' AND read_as_ensemble_type IS NOT NULL
            AND residue_key IS NULL AND residues IS NULL AND multi_conformer_residues IS NULL)
        OR (residues_version IS NOT NULL AND conformers = 'ensemble_model_type' AND read_as_ensemble_type = true
            AND residue_key IS NULL AND residues IS NULL AND multi_conformer_residues IS NULL),
        false)
    ) NOT VALID;
