ALTER TABLE entry_search_index DROP CONSTRAINT entry_search_index_entry_id_fkey;
ALTER TABLE entry_revisions DROP CONSTRAINT entry_revisions_entry_id_fkey;
ALTER TABLE models DROP CONSTRAINT models_entry_id_fkey;
ALTER TABLE model_revisions DROP CONSTRAINT model_revisions_model_id_fkey;

ALTER TABLE entries
    ALTER COLUMN id TYPE text USING id::text;
ALTER TABLE entry_search_index
    ALTER COLUMN entry_id TYPE text USING entry_id::text;
ALTER TABLE entry_revisions
    ALTER COLUMN entry_id TYPE text USING entry_id::text;
ALTER TABLE models
    ALTER COLUMN id TYPE text USING id::text,
    ALTER COLUMN entry_id TYPE text USING entry_id::text;
ALTER TABLE model_revisions
    ALTER COLUMN model_id TYPE text USING model_id::text;

ALTER TABLE entry_search_index
    ADD CONSTRAINT entry_search_index_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES entries(id);
ALTER TABLE entry_revisions
    ADD CONSTRAINT entry_revisions_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES entries(id);
ALTER TABLE models
    ADD CONSTRAINT models_entry_id_fkey
    FOREIGN KEY (entry_id) REFERENCES entries(id);
ALTER TABLE model_revisions
    ADD CONSTRAINT model_revisions_model_id_fkey
    FOREIGN KEY (model_id) REFERENCES models(id);
