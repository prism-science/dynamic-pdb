ALTER TABLE entries
    ADD COLUMN created_by uuid NOT NULL REFERENCES users(id);

ALTER TABLE models
    ADD COLUMN created_by uuid NOT NULL REFERENCES users(id);

CREATE INDEX entries_created_by_idx
    ON entries(created_by);

CREATE INDEX models_created_by_idx
    ON models(created_by);
