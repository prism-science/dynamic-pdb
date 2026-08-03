CREATE TABLE users (
    id            uuid PRIMARY KEY,

    source        text NOT NULL,
    external_ref  text NOT NULL,

    email         text,
    display_name  text,
    avatar_url    text,

    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL
);

CREATE TABLE entries (
    id          uuid PRIMARY KEY,

    created_by  uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX entries_created_at_idx
    ON entries(created_at);

CREATE INDEX entries_created_by_created_at_idx
    ON entries(created_by, created_at);

CREATE TABLE entry_search_index (
    entry_id    uuid NOT NULL REFERENCES entries(id),
    model_type  text NOT NULL,
    model_id    text NOT NULL,
    updated_at  timestamptz NOT NULL,
    search_text text NOT NULL,
    search_tsv  tsvector NOT NULL,

    PRIMARY KEY (entry_id, model_type, model_id)
);

CREATE INDEX entry_search_index_search_tsv_idx
    ON entry_search_index
    USING GIN (search_tsv);

CREATE TABLE artifacts (
    id          uuid PRIMARY KEY,

    name        text NOT NULL,
    level       text NOT NULL,
    uri         text,
    sha256      text,
    format      text,
    size_bytes  bigint,
    metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_by  uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX artifacts_created_by_created_at_idx
    ON artifacts(created_by, created_at);

CREATE TABLE entry_revisions (
    id                  uuid PRIMARY KEY,
    entry_id            uuid NOT NULL REFERENCES entries(id),
    parent_revision_id  uuid REFERENCES entry_revisions(id),

    revision_number     integer,
    state               text NOT NULL DEFAULT 'pending',
    change_summary      text,
    published_at        timestamptz,

    name                text NOT NULL,
    description         text,
    thumbnail_image_url text,
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_by          uuid NOT NULL REFERENCES users(id),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX entry_revisions_active_idx
    ON entry_revisions(entry_id, state)
    WHERE state = 'active';

CREATE UNIQUE INDEX entry_revisions_entry_id_revision_number_idx
    ON entry_revisions(entry_id, revision_number);

CREATE INDEX entry_revisions_parent_revision_id_idx
    ON entry_revisions(parent_revision_id);

CREATE INDEX entry_revisions_created_by_created_at_idx
    ON entry_revisions(created_by, created_at);

CREATE INDEX entry_revisions_in_review_idx
    ON entry_revisions(created_at)
    WHERE state = 'in_review';

CREATE TABLE entry_revision_artifacts (
    entry_revision_id uuid NOT NULL REFERENCES entry_revisions(id),
    artifact_id       uuid NOT NULL REFERENCES artifacts(id),

    PRIMARY KEY (entry_revision_id, artifact_id)
);

CREATE TABLE protein_sequences (
    id                  uuid PRIMARY KEY,
    entry_revision_id   uuid NOT NULL REFERENCES entry_revisions(id),
    source_artifact_id  uuid NOT NULL REFERENCES artifacts(id),
    record_index        integer NOT NULL,
    header              text NOT NULL,
    sequence            text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX protein_sequences_revision_artifact_record_idx
    ON protein_sequences(entry_revision_id, source_artifact_id, record_index);

CREATE TABLE models (
    id          uuid PRIMARY KEY,
    entry_id    uuid NOT NULL REFERENCES entries(id),

    created_by  uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX models_entry_id_created_at_idx
    ON models(entry_id, created_at);

CREATE INDEX models_created_by_created_at_idx
    ON models(created_by, created_at);

CREATE TABLE model_revisions (
    id                  uuid PRIMARY KEY,
    model_id            uuid NOT NULL REFERENCES models(id),
    parent_revision_id  uuid REFERENCES model_revisions(id),
    primary_artifact_id uuid REFERENCES artifacts(id),

    revision_number     integer,
    state               text NOT NULL DEFAULT 'pending',
    change_summary      text,
    published_at        timestamptz,

    name                text NOT NULL,
    description         text,
    thumbnail_image_url text,
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_by          uuid NOT NULL REFERENCES users(id),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX model_revisions_active_idx
    ON model_revisions(model_id, state)
    WHERE state = 'active';

CREATE UNIQUE INDEX model_revisions_model_id_revision_number_idx
    ON model_revisions(model_id, revision_number);

CREATE INDEX model_revisions_parent_revision_id_idx
    ON model_revisions(parent_revision_id);

CREATE INDEX model_revisions_created_by_created_at_idx
    ON model_revisions(created_by, created_at);

CREATE INDEX model_revisions_in_review_idx
    ON model_revisions(created_at)
    WHERE state = 'in_review';

CREATE TABLE model_revision_artifacts (
    model_revision_id uuid NOT NULL REFERENCES model_revisions(id),
    artifact_id       uuid NOT NULL REFERENCES artifacts(id),

    PRIMARY KEY (model_revision_id, artifact_id)
);

CREATE TABLE metrics (
    id          uuid PRIMARY KEY,
    key         text NOT NULL,
    value       numeric NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE model_revision_metrics (
    model_revision_id uuid NOT NULL REFERENCES model_revisions(id),
    metric_id         uuid NOT NULL REFERENCES metrics(id),

    PRIMARY KEY (model_revision_id, metric_id)
);

CREATE TABLE runs (
    id                uuid PRIMARY KEY,

    name              text NOT NULL,
    software_name     text,
    software_version  text,

    command           text,
    parameters        jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,

    started_at        timestamptz,
    finished_at       timestamptz,

    created_by        uuid NOT NULL REFERENCES users(id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX runs_created_by_idx
    ON runs(created_by);

CREATE TABLE model_revision_runs (
    model_revision_id uuid NOT NULL REFERENCES model_revisions(id),
    run_id            uuid NOT NULL REFERENCES runs(id),

    PRIMARY KEY (model_revision_id, run_id)
);

CREATE TABLE run_artifacts (
    run_id      uuid NOT NULL REFERENCES runs(id),
    artifact_id uuid NOT NULL REFERENCES artifacts(id),
    direction   text NOT NULL,

    PRIMARY KEY (run_id, artifact_id)
);

CREATE INDEX run_artifacts_artifact_id_idx
    ON run_artifacts(artifact_id);
