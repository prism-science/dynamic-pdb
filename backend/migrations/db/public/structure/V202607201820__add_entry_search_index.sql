CREATE TABLE entry_search_index (
    entry_id    uuid NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
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
