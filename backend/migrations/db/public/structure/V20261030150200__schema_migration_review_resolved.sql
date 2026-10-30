-- A review row is resolved when a later job settles what the migration could not, such as the
-- archive re-derivation job (T1-C2) deriving an archive entry's lists from its RCSB file.
-- resolved_by names that job; unresolved rows keep NULL in both. Resolved rows stay, so the
-- inventory can still list what each migration flagged.
--
-- Both columns are nullable with no default, so ADD COLUMN changes only the catalog. The check is
-- added NOT VALID, which skips the scan; the next migration validates it. The table is written only
-- by migrations and the job. lock_timeout makes the migration fail cleanly (nothing applied,
-- rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE schema_migration_review ADD COLUMN resolved_at timestamptz;
ALTER TABLE schema_migration_review ADD COLUMN resolved_by text;
ALTER TABLE schema_migration_review
    ADD CONSTRAINT schema_migration_review_resolved_check
    CHECK ((resolved_at IS NULL) = (resolved_by IS NULL)) NOT VALID;
