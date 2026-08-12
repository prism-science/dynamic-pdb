ALTER TABLE entries         ADD COLUMN state text NOT NULL DEFAULT 'new';
ALTER TABLE models          ADD COLUMN state text NOT NULL DEFAULT 'new';
ALTER TABLE entry_revisions ADD COLUMN entry_state text NOT NULL DEFAULT 'new';
ALTER TABLE model_revisions ADD COLUMN model_state text NOT NULL DEFAULT 'new';

UPDATE entries         SET state       = 'active';
UPDATE models          SET state       = 'active';
UPDATE entry_revisions SET entry_state = 'active';
UPDATE model_revisions SET model_state = 'active';
