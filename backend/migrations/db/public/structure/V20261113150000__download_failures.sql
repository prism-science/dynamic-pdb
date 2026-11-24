-- Failure reporting for bulk-download archives (L2).
--
-- download_failures lists, per finished archive, the selected files it could not vouch for: the
-- ones its manifest lists as unavailable or changed-at-source, with the reason code. Linked files
-- (no copy here by design, for example over 1 GiB) are not failures and are not listed. A row
-- names the first selected model that lists the file. Normally an archive has a handful of rows
-- or none; they are written once, when the archive becomes ready, and never change.
--
-- retryable is true for a file Dynamic PDB holds a matching copy of that could not be read while
-- the archive was prepared (read_failed): asking again may succeed. POST
-- /v1/downloads/{id}/retry builds a second, small archive of only those files, a job whose
-- retry_of names the archive it completes.
--
-- files_retryable on download_jobs counts a ready archive's retryable failures, so reading a job
-- never counts rows.
--
-- New table, and new columns without defaults to fill on a table that is empty everywhere
-- (downloads are off). lock_timeout makes the migration fail cleanly (nothing applied, rerun it)
-- instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE download_jobs
    ADD COLUMN retry_of uuid REFERENCES download_jobs(id),
    ADD COLUMN files_retryable integer CHECK (files_retryable >= 0);

-- A request for a selection's archive never reuses a retry archive, and a retry request finds
-- the parent's latest retry.
CREATE INDEX download_jobs_retry_of_idx ON download_jobs (retry_of, created_at DESC) WHERE retry_of IS NOT NULL;

CREATE TABLE download_failures (
    job_id      uuid NOT NULL REFERENCES download_jobs(id),
    file_id     uuid NOT NULL,
    ordinal     integer NOT NULL CHECK (ordinal > 0),
    entry_id    text NOT NULL,
    model_id    text NOT NULL,
    file_name   text NOT NULL,
    reason_code text NOT NULL CHECK (reason_code IN (
        'stored_file_differs', 'changed_at_source', 'source_unreadable', 'copy_not_in_storage',
        'copy_missing', 'copy_differs', 'read_failed')),
    retryable   boolean NOT NULL,
    PRIMARY KEY (job_id, file_id),
    CONSTRAINT download_failures_retryable_check CHECK (retryable = (reason_code = 'read_failed'))
);

-- GET /v1/downloads/{id}/failures pages through a job's failures in selection order.
CREATE INDEX download_failures_page_idx ON download_failures (job_id, ordinal, file_id);
