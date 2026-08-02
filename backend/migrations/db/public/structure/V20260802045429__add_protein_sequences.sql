DROP INDEX IF EXISTS entities_fasta_sequence_trgm_idx;

CREATE TABLE protein_sequences (
    id           uuid PRIMARY KEY,
    entry_id     uuid NOT NULL REFERENCES entries(id),
    entity_id    uuid NOT NULL REFERENCES entities(id),
    record_index integer NOT NULL,
    header       text NOT NULL,
    sequence     text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (entity_id, record_index)
);

CREATE INDEX protein_sequences_entry_id_idx
    ON protein_sequences(entry_id);

CREATE INDEX protein_sequences_sequence_trgm_idx
    ON protein_sequences
    USING GIN (sequence gin_trgm_ops);
