-- What the checksum job found for each artifact: the sha256 and size it computed from the
-- bytes, the outcome, and where our copy of an external file is stored. The uploader's
-- declared sha256 and size stay on artifacts; nothing here changes them.
--
-- status: pending (not checked yet, or waiting for a retry), verified (the bytes match the
-- declared sha256 and size, or nothing was declared), mismatch (the declared sha256 or size
-- differs), failed (the bytes could not be read).
--
-- next_attempt_at is when the job may take a pending row. A claim moves it ahead by the
-- lease, so a row held by a job that crashed becomes due again by itself.
--
-- The table starts empty. New artifacts get a pending row when they are created, and the job
-- adds one for every artifact without a row at the start of each run.
--
-- The foreign key takes SHARE ROW EXCLUSIVE on artifacts for milliseconds; lock_timeout makes
-- the migration fail cleanly (nothing applied, rerun it) instead of queueing behind a long
-- transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE artifact_checksums (
    artifact_id      uuid PRIMARY KEY REFERENCES artifacts(id),
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'verified', 'mismatch', 'failed')),
    sha256           text CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes       bigint CHECK (size_bytes >= 0),
    stored_uri       text,
    attempts         integer NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    last_error       text,
    checked_at       timestamptz,
    CONSTRAINT artifact_checksums_result_check CHECK (
        status NOT IN ('verified', 'mismatch')
        OR (sha256 IS NOT NULL AND size_bytes IS NOT NULL AND checked_at IS NOT NULL)
    )
);

-- Serves the job's claim, which takes the oldest due rows in (next_attempt_at, artifact_id)
-- order; finished rows leave the index.
CREATE INDEX artifact_checksums_due_idx
    ON artifact_checksums(next_attempt_at, artifact_id)
    WHERE status = 'pending';
