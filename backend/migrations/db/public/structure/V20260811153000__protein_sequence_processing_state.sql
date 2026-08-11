ALTER TABLE protein_sequences
    ADD COLUMN processing_state text NOT NULL DEFAULT 'pending';
