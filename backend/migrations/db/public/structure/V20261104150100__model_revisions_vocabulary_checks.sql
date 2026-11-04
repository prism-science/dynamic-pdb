-- T1-E: the database refuses a model_type or purpose outside Dynamic PDB Schema v0.2.0, the
-- same lists as models.StructureModelType.Validate and models.ModelPurpose.Validate. A missing
-- value is still allowed, as in the schema.
--
-- NOT VALID: no scan (V20261104150500 and V20261104150600 validate the existing rows). A NOT
-- VALID check still applies to an UPDATE of an existing row, and the app archives the previous
-- active revision with an UPDATE, so the inventory checks 'model_invalid_type' and
-- 'model_invalid_purpose' must be empty before this ships (prod: 0 rows on 2026-11-04).
--
-- ACCESS EXCLUSIVE on model_revisions for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE model_revisions
    ADD CONSTRAINT model_revisions_model_type_chk CHECK (
        metadata ->> 'model_type' IS NULL
        OR metadata ->> 'model_type' IN ('Single Conformer', 'Multiconformer', 'Ensemble', 'Deposited')) NOT VALID,
    ADD CONSTRAINT model_revisions_purpose_chk CHECK (
        metadata ->> 'purpose' IS NULL
        OR metadata ->> 'purpose' IN ('Model Building', 'Refinement', 'Reference')) NOT VALID;
