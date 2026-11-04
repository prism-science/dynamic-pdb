-- T1-E: the database refuses an artifact sha256 that is not 64 lowercase hex characters, and a
-- negative size. Both stay optional, as in Dynamic PDB Schema v0.2.0.
--
-- NOT VALID: no scan (V20261104150700 and V20261104150800 validate the existing rows). A NOT
-- VALID check still applies to an UPDATE of an existing row, so the inventory checks
-- 'artifact_malformed_sha256' and 'artifact_negative_size' must be empty before this ships
-- (prod: 0 rows on 2026-11-04).
--
-- ACCESS EXCLUSIVE on artifacts for milliseconds; lock_timeout makes the migration fail cleanly
-- (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_sha256_format_chk CHECK (sha256 IS NULL OR sha256 ~ '^[0-9a-f]{64}$') NOT VALID,
    ADD CONSTRAINT artifacts_size_bytes_chk CHECK (size_bytes IS NULL OR size_bytes >= 0) NOT VALID;
