-- Conformer counts of each model file, read by the coordinate-index job in the pass that reads its
-- chains (cif.ReadCoordinateIndex, S3). Researcher example search 1, "a lysine with more than three
-- conformations", reads them (S4).
--
-- coordinate_index_residue_counts: one row per (model file, residue type, conformer count) with the
-- number of residues of that type with that many conformers, in the file's first model. Every
-- polymer residue is in exactly one row, so a file's rows add up to coordinate_index_sets.residues.
-- A residue is label chain + label residue number + residue type, or, in files without label
-- numbering, author chain + residue number + insertion code + residue type (residue_key). Its
-- conformer count is the number of its alternate location letters whose highest occupancy on a heavy
-- atom of the residue is at least 0.1, minimum 1; letters found only on hydrogens never count.
-- Keyed by model file, not model revision, as coordinate_index_chains: the search joins
-- model_revisions.primary_artifact_id.
--
-- Ensembles get no rows (their conformer count is null): a file with more than one model frame
-- (conformers 'ensemble_model_frames'), or the file of a model whose latest revision using it is an
-- Ensemble or has member_count (conformers 'ensemble_model_type').
--
-- New columns of coordinate_index_sets, all null until the job reads the file with conformer counts
-- on (they keep the last such read while the set is pending again):
--   residues_version: cif.ResidueCountVersion of that read.
--   conformers: counted, ensemble_model_frames or ensemble_model_type.
--   residue_key: label, author or mixed; null when nothing was counted.
--   residues, multi_conformer_residues: the file's polymer residues, and those with 2 or more
--     conformers (the residue-level altloc_fraction is their ratio); null for ensembles.
--   read_as_ensemble_type: whether a model using the file was an Ensemble at that read; a later
--     revision that changes it queues the file again.
--
-- A CHECK passes when its expression is NULL, so a NULL in a column a branch needs (residues of a
-- counted read, say) would make that branch NULL instead of false and slip through: coalesce turns
-- NULL into false, so a row passes only when one branch is true.
--
-- ADD COLUMN without a default changes only the catalog. The check is added NOT VALID (no scan;
-- it still applies to every insert and update) and validated by V20261110210100. The foreign key
-- takes SHARE ROW EXCLUSIVE on coordinate_index_sets for milliseconds. lock_timeout makes the
-- migration fail cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE coordinate_index_sets
    ADD COLUMN residues_version         text,
    ADD COLUMN conformers               text,
    ADD COLUMN residue_key              text,
    ADD COLUMN residues                 integer,
    ADD COLUMN multi_conformer_residues integer,
    ADD COLUMN read_as_ensemble_type    boolean,
    ADD CONSTRAINT coordinate_index_sets_conformers_check CHECK (coalesce(
        (residues_version IS NULL AND conformers IS NULL AND residue_key IS NULL AND residues IS NULL
            AND multi_conformer_residues IS NULL AND read_as_ensemble_type IS NULL)
        OR (residues_version IS NOT NULL AND conformers = 'counted' AND read_as_ensemble_type = false
            AND residues >= 0 AND multi_conformer_residues BETWEEN 0 AND residues
            AND (residue_key IS NULL) = (residues = 0)
            AND (residue_key IS NULL OR residue_key IN ('label', 'author', 'mixed')))
        OR (residues_version IS NOT NULL AND conformers = 'ensemble_model_frames' AND read_as_ensemble_type IS NOT NULL
            AND residue_key IS NULL AND residues IS NULL AND multi_conformer_residues IS NULL)
        OR (residues_version IS NOT NULL AND conformers = 'ensemble_model_type' AND read_as_ensemble_type = true
            AND residue_key IS NULL AND residues IS NULL AND multi_conformer_residues IS NULL),
        false)
    ) NOT VALID;

CREATE TABLE coordinate_index_residue_counts (
    artifact_id     uuid     NOT NULL REFERENCES coordinate_index_sets(artifact_id),
    residue_type    text     NOT NULL,
    conformer_count smallint NOT NULL CHECK (conformer_count BETWEEN 1 AND 16),
    residues        integer  NOT NULL CHECK (residues > 0),
    PRIMARY KEY (artifact_id, residue_type, conformer_count)
);

-- V20261110210200 adds the search index.
