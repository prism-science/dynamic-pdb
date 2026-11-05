-- The database refuses a model origin outside the list in models.ModelOrigin.Validate. A missing
-- value is allowed: only an archive deposition keeping a representation as its model_type names it.
--
-- NOT VALID: no scan (V20261105120100 validates the existing rows). The inventory check
-- 'model_invalid_origin' must be empty before this ships. origin is a new key, and the API refuses
-- metadata keys it does not know, so no stored row should carry it.
--
-- ACCESS EXCLUSIVE on model_revisions for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE model_revisions
    ADD CONSTRAINT model_revisions_origin_chk CHECK (
        metadata ->> 'origin' IS NULL
        OR metadata ->> 'origin' IN ('archive')) NOT VALID;
