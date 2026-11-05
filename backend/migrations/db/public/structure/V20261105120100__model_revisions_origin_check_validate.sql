-- Validates model_revisions_origin_chk, which V20261105120000 added NOT VALID. VALIDATE takes
-- SHARE UPDATE EXCLUSIVE on model_revisions: the API and the jobs keep reading and writing while it
-- scans the table once, and it writes nothing. One statement per migration, so each runs in its own
-- transaction. If a row breaks the check, the migration fails and applies nothing; the check stays
-- NOT VALID. The inventory check 'model_invalid_origin' lists such rows.
-- lock_timeout makes it fail cleanly instead of queueing behind other DDL on the table.
SET LOCAL lock_timeout = '5s';

ALTER TABLE model_revisions VALIDATE CONSTRAINT model_revisions_origin_chk;
