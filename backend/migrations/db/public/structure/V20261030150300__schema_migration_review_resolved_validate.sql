-- Validates the check the previous migration added NOT VALID. VALIDATE takes SHARE UPDATE
-- EXCLUSIVE on schema_migration_review while it scans the table once; every row passes, since
-- the new columns are all NULL at this point.
ALTER TABLE schema_migration_review VALIDATE CONSTRAINT schema_migration_review_resolved_check;
