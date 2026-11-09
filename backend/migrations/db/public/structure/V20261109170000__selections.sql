-- Frozen selections (B1): a researcher's selection of models, saved with the model revision and
-- entry revision that were active when it was saved. Downloads and their previews read only
-- these rows, so a selection names the same files however the catalog changes later. Selections
-- are public to anyone with their random ID, need no sign-in and are kept indefinitely.
--
-- request is what the client asked for, normalized: {"model_ids": [...], "searches": [...],
-- "exclude_model_ids": [...]}, each search being a GET /v1/models query string. model_count and
-- entry_count count the frozen rows. charged_rows is what the selection counts against the
-- storage budget of all selections (selections.max_stored_models): its frozen rows plus its
-- stored request, which can hold up to 20,000 model IDs, as rows of about 120 bytes.
--
-- New, empty tables: nothing is rewritten and no existing row is checked. The foreign keys take
-- SHARE ROW EXCLUSIVE on model_revisions and entry_revisions for milliseconds; lock_timeout
-- makes the migration fail cleanly (nothing applied, rerun it) instead of queueing behind a long
-- transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE selections (
    id           uuid PRIMARY KEY,
    model_count  integer NOT NULL CHECK (model_count > 0),
    entry_count  integer NOT NULL CHECK (entry_count > 0 AND entry_count <= model_count),
    charged_rows integer NOT NULL CHECK (charged_rows > model_count),
    request      jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- One row per model, numbered in the order GET /v1/models lists models (model revision
-- created_at, id). Only active revisions are frozen, one per model, so a model appears once.
-- The foreign keys keep the frozen revisions: nothing deletes revisions, and code that ever did
-- would fail here instead of breaking saved selections. About 120 bytes per row with the
-- primary key: a selection of the whole catalog (225k models) is about 27 MB.
CREATE TABLE selection_models (
    selection_id       uuid    NOT NULL REFERENCES selections(id),
    ordinal            integer NOT NULL CHECK (ordinal > 0),
    model_revision_id  uuid    NOT NULL REFERENCES model_revisions(id),
    entry_revision_id  uuid    NOT NULL REFERENCES entry_revisions(id),
    PRIMARY KEY (selection_id, ordinal)
);
