CREATE TABLE data_sync_jobs (
    entry_id     text NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    model_id     text REFERENCES models(id) ON DELETE CASCADE,
    scheduled_at timestamptz NOT NULL,

    CONSTRAINT data_sync_jobs_entry_model_key
        UNIQUE NULLS NOT DISTINCT (entry_id, model_id)
);

CREATE INDEX data_sync_jobs_scheduled_at_idx
    ON data_sync_jobs(scheduled_at);
