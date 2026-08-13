CREATE INDEX CONCURRENTLY protein_sequences_pending_id_idx
    ON protein_sequences(id)
    WHERE processing_state = 'pending';
