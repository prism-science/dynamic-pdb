-- Validates metrics_key_known_chk, which V20261104150000 added NOT VALID. VALIDATE takes
-- SHARE UPDATE EXCLUSIVE on metrics: the API and the jobs keep reading and writing while it
-- scans the table once, and it writes nothing. One statement per migration, so each runs in
-- its own transaction. If a row breaks the check, the migration fails and applies nothing; the
-- check stays NOT VALID.
-- The inventory check 'metric_unknown_key' lists such rows; on prod it listed none on 2026-11-04.
-- lock_timeout makes it fail cleanly instead of queueing behind other DDL on the table.
SET LOCAL lock_timeout = '5s';

ALTER TABLE metrics VALIDATE CONSTRAINT metrics_key_known_chk;
