-- The researchers' answers of 17 Nov for bulk download (patch 23).
--
-- Q9, unknown file sizes: the checksum job asks a file's source for its size (an HTTP HEAD
-- request) when it cannot read the file, and keeps the answer in source_size_bytes, with the time
-- in source_size_checked_at. Only pending and failed rows get it: B1b's trigger
-- (V20261110170800) keeps a verified or mismatch row's status, SHA-256, size and check time, and
-- such a row already has the size read from the bytes. The size is what the source reports, never
-- an estimate; archives list it for files they link.
--
-- Item 14, files whose original bytes are gone: archives mark them unavailable (reason code
-- original_bytes_unavailable) instead of changed-at-source, so download_jobs no longer counts
-- changed-at-source files and download_failures takes the new code.
--
-- artifact_checksums: two nullable columns without a default, so prod's ~570,000 rows are not
-- rewritten; the CHECK on source_size_bytes is verified by one scan of the table while the lock is
-- held (every value is NULL, so it passes). download_jobs and download_failures are empty
-- everywhere (downloads have never been on), so their checks are re-created on empty tables.
-- lock_timeout makes the migration fail cleanly (nothing applied, rerun it) instead of queueing
-- behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE artifact_checksums
    ADD COLUMN source_size_bytes bigint CHECK (source_size_bytes >= 0),
    ADD COLUMN source_size_checked_at timestamptz;

ALTER TABLE download_jobs DROP CONSTRAINT download_jobs_status_fields_check;
ALTER TABLE download_jobs DROP COLUMN files_changed_at_source;
ALTER TABLE download_jobs ADD CONSTRAINT download_jobs_status_fields_check CHECK (
    CASE status
        WHEN 'queued' THEN lease_until IS NULL AND finished_at IS NULL
        WHEN 'planning' THEN lease_until IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL
        WHEN 'planned' THEN lease_until IS NULL AND started_at IS NOT NULL AND finished_at IS NULL
            AND planned_bytes IS NOT NULL AND planned_bytes <= max_archive_bytes AND preview IS NOT NULL
        WHEN 'preparing' THEN lease_until IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL
            AND planned_bytes IS NOT NULL AND preview IS NOT NULL
        WHEN 'ready' THEN lease_until IS NULL AND finished_at IS NOT NULL AND expires_at > finished_at
            AND planned_bytes IS NOT NULL AND preview IS NOT NULL
            AND archive_key IS NOT NULL AND archive_url IS NOT NULL
            AND archive_bytes IS NOT NULL AND archive_sha256 IS NOT NULL
            AND manifest_key IS NOT NULL AND manifest_url IS NOT NULL
            AND files_included IS NOT NULL AND files_linked IS NOT NULL AND files_unavailable IS NOT NULL
        WHEN 'blocked' THEN lease_until IS NULL AND finished_at IS NOT NULL
            AND planned_bytes > max_archive_bytes AND blocked_reason IS NOT NULL AND preview IS NOT NULL
        WHEN 'failed' THEN lease_until IS NULL AND finished_at IS NOT NULL AND error IS NOT NULL
    END
);

-- The reason-code CHECK that V20261113150000 declared inline; Postgres named it.
ALTER TABLE download_failures DROP CONSTRAINT download_failures_reason_code_check;
ALTER TABLE download_failures ADD CONSTRAINT download_failures_reason_code_check CHECK (reason_code IN (
    'stored_file_differs', 'original_bytes_unavailable', 'source_unreadable', 'copy_not_in_storage',
    'copy_missing', 'copy_differs', 'read_failed'));
