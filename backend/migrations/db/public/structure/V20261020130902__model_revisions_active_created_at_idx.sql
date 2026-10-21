CREATE INDEX CONCURRENTLY model_revisions_active_created_at_id_idx
    ON model_revisions(created_at, id)
    WHERE state = 'active';
