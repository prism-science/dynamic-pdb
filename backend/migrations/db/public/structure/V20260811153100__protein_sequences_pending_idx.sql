CREATE INDEX CONCURRENTLY protein_sequences_pending_created_at_idx
    ON protein_sequences(created_at)
    WHERE processing_state = 'pending';
