-- Bulk-download archives (B2): one row per request to build a zip of a frozen selection's files.
-- POST /v1/downloads adds a queued row; the archive worker claims it, plans the archive from the
-- selection's pinned revisions and files, and either blocks it (over the size limit) or writes
-- the zip and its manifest to the data bucket under downloads/<id>/ and marks it ready. The
-- browser polls GET /v1/downloads/{id}. Rows are public to anyone with their random ID, like
-- selections.
--
-- status: queued (waiting for the worker), planning (the worker reads the selection and adds up
-- the bytes), preparing (the worker writes the zip), ready (the zip and manifest are in the
-- bucket until expires_at), blocked (the files to include add up to more than the limit; nothing
-- was written), failed (the worker gave up; error says why). A ready archive past expires_at is
-- reported as expired without a write: the bucket's lifecycle rule deletes the objects, and the
-- row and the selection stay.
--
-- lease_until and attempts let a worker claim a row: a claim sets status planning, takes a lease
-- and counts the attempt; a worker that dies leaves the lease to run out, and the next claim
-- takes the row again. Every later write of the worker names the attempt it claimed, so a worker
-- that lost its lease cannot overwrite a newer attempt.
--
-- The files_* counts are the manifest's file rows by status; per-file results live only in the
-- manifest object in the bucket, so this table grows by one small row (about 400 bytes) per
-- request.
--
-- New, empty table: nothing is rewritten and no existing row is checked. The foreign key takes
-- SHARE ROW EXCLUSIVE on selections for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE download_jobs (
    id                      uuid PRIMARY KEY,
    selection_id            uuid NOT NULL REFERENCES selections(id),
    file_choice             text NOT NULL
                            CHECK (file_choice IN ('original_mmcif', 'modified_coordinates', 'all_files')),
    status                  text NOT NULL DEFAULT 'queued'
                            CHECK (status IN ('queued', 'planning', 'preparing', 'ready', 'blocked', 'failed')),
    max_archive_bytes       bigint NOT NULL CHECK (max_archive_bytes > 0),
    created_at              timestamptz NOT NULL DEFAULT statement_timestamp(),
    started_at              timestamptz,
    finished_at             timestamptz,
    expires_at              timestamptz,
    lease_until             timestamptz,
    attempts                integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    planned_bytes           bigint CHECK (planned_bytes >= 0),
    archive_key             text,
    archive_url             text,
    archive_bytes           bigint CHECK (archive_bytes > 0),
    archive_sha256          text CHECK (archive_sha256 ~ '^[0-9a-f]{64}$'),
    manifest_key            text,
    manifest_url            text,
    files_included          integer CHECK (files_included >= 0),
    files_linked            integer CHECK (files_linked >= 0),
    files_unavailable       integer CHECK (files_unavailable >= 0),
    files_changed_at_source integer CHECK (files_changed_at_source >= 0),
    blocked_reason          text,
    error                   text,
    CONSTRAINT download_jobs_status_fields_check CHECK (
        CASE status
            WHEN 'queued' THEN lease_until IS NULL AND finished_at IS NULL
            WHEN 'planning' THEN lease_until IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL
            WHEN 'preparing' THEN lease_until IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL
                AND planned_bytes IS NOT NULL
            WHEN 'ready' THEN lease_until IS NULL AND finished_at IS NOT NULL AND expires_at > finished_at
                AND planned_bytes IS NOT NULL
                AND archive_key IS NOT NULL AND archive_url IS NOT NULL
                AND archive_bytes IS NOT NULL AND archive_sha256 IS NOT NULL
                AND manifest_key IS NOT NULL AND manifest_url IS NOT NULL
                AND files_included IS NOT NULL AND files_linked IS NOT NULL
                AND files_unavailable IS NOT NULL AND files_changed_at_source IS NOT NULL
            WHEN 'blocked' THEN lease_until IS NULL AND finished_at IS NOT NULL
                AND planned_bytes > max_archive_bytes AND blocked_reason IS NOT NULL
            WHEN 'failed' THEN lease_until IS NULL AND finished_at IS NOT NULL AND error IS NOT NULL
        END
    )
);

-- The worker's claim: the oldest queued row, or a row whose worker's lease ran out. Finished rows
-- leave the index.
CREATE INDEX download_jobs_unfinished_idx
    ON download_jobs (created_at, id)
    WHERE status IN ('queued', 'planning', 'preparing');

-- A repeated request finds the selection's latest archive of the same file choice, and the
-- per-selection cap counts its unfinished archives.
CREATE INDEX download_jobs_selection_idx
    ON download_jobs (selection_id, file_choice, created_at DESC);
