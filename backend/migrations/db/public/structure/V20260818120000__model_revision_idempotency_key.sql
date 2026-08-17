ALTER TABLE model_revisions
    ADD COLUMN idempotency_key text;

CREATE UNIQUE INDEX model_revisions_active_idempotency_key_idx
    ON model_revisions(idempotency_key)
    WHERE idempotency_key IS NOT NULL
      AND state = 'active';
