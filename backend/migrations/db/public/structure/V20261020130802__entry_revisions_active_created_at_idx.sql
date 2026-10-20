CREATE INDEX CONCURRENTLY entry_revisions_active_created_at_id_idx
    ON entry_revisions(created_at DESC, id DESC)
    WHERE state = 'active';
