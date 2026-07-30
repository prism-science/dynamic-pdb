CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX entities_fasta_sequence_trgm_idx
    ON entities
    USING GIN ((upper(payload #>> '{metadata,sequence}')) gin_trgm_ops)
    WHERE type = 'data'
      AND payload ->> 'type' = 'fasta'
      AND jsonb_typeof(payload #> '{metadata,sequence}') = 'string';
