-- Download preview (L1): a download job can stop after planning and show what its archive would
-- hold before anything is built.
--
-- POST /v1/downloads with prepare false (the website) plans the archive and rests at status
-- planned with its preview: record and file counts, sizes, availability and checksum status of
-- the selection's files for the file choice. POST /v1/downloads/{id}/prepare sets
-- prepare_requested, and the worker's building lane takes it from there. With prepare true (the
-- default, as before) the job goes on to building on its own once planned.
--
-- status gains planned: planned_bytes and preview are set, no lease, not finished. A planned job
-- with prepare_requested waits for the building lane; one without it waits for the person.
--
-- failed_attempts counts the failed attempts of the current phase (planning, then building):
-- the worker fails the job when it reaches archive_worker::max_attempts, and it starts again at 0
-- when planning ends. attempts stays the claim counter every worker write names, so an attempt
-- that lost its lease can never write over a later one.
--
-- preview is about 1-2 KB of JSON per job: counts and byte totals only, no per-file rows.
--
-- download_jobs is new and empty on every environment (downloads are off), so the constraint and
-- index changes check and rewrite nothing. lock_timeout makes the migration fail cleanly (nothing
-- applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE download_jobs
    ADD COLUMN prepare_requested boolean NOT NULL DEFAULT true,
    ADD COLUMN preview jsonb,
    ADD COLUMN failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0);

-- The column CHECK on status that V20261111190000 declared inline; Postgres named it.
ALTER TABLE download_jobs DROP CONSTRAINT download_jobs_status_check;
ALTER TABLE download_jobs ADD CONSTRAINT download_jobs_status_check
    CHECK (status IN ('queued', 'planning', 'planned', 'preparing', 'ready', 'blocked', 'failed'));

ALTER TABLE download_jobs DROP CONSTRAINT download_jobs_status_fields_check;
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
            AND files_included IS NOT NULL AND files_linked IS NOT NULL
            AND files_unavailable IS NOT NULL AND files_changed_at_source IS NOT NULL
        WHEN 'blocked' THEN lease_until IS NULL AND finished_at IS NOT NULL
            AND planned_bytes > max_archive_bytes AND blocked_reason IS NOT NULL AND preview IS NOT NULL
        WHEN 'failed' THEN lease_until IS NULL AND finished_at IS NOT NULL AND error IS NOT NULL
    END
);

-- The worker's two lanes claim from these. Planning: queued rows, or planning rows whose lease ran
-- out. Building: planned rows the person asked to prepare, or preparing rows whose lease ran out.
DROP INDEX download_jobs_unfinished_idx;
CREATE INDEX download_jobs_planning_idx
    ON download_jobs (created_at, id)
    WHERE status IN ('queued', 'planning');
CREATE INDEX download_jobs_building_idx
    ON download_jobs (created_at, id)
    WHERE status = 'preparing' OR (status = 'planned' AND prepare_requested);
