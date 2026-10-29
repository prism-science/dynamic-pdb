-- Validates the CHECK the copy status migration added NOT VALID. VALIDATE takes SHARE UPDATE
-- EXCLUSIVE on artifact_checksums, so the API keeps reading and writing while it scans the table
-- once, and it writes nothing. Every row passes: the previous migration gave each verified and
-- mismatch row its copy status, and pending and failed rows have none. lock_timeout keeps it
-- from queueing behind other DDL.
SET LOCAL lock_timeout = '5s';

ALTER TABLE artifact_checksums VALIDATE CONSTRAINT artifact_checksums_copy_status_check;
