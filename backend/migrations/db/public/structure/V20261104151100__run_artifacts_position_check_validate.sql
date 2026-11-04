-- Validates run_artifacts_position_chk, which V20261023090700 added NOT VALID. VALIDATE takes
-- SHARE UPDATE EXCLUSIVE on run_artifacts: the API and the jobs keep reading and writing while it
-- scans the table once, and it writes nothing. One statement per migration, so each runs in
-- its own transaction. If a row breaks the check, the migration fails and applies nothing; the
-- check stays NOT VALID.
-- `select * from run_artifacts where position < 0` lists such rows. The column came with the
-- check and nothing writes it yet (db/runs.go inserts run_id, artifact_id, direction only).
-- lock_timeout makes it fail cleanly instead of queueing behind other DDL on the table.
SET LOCAL lock_timeout = '5s';

ALTER TABLE run_artifacts VALIDATE CONSTRAINT run_artifacts_position_chk;
