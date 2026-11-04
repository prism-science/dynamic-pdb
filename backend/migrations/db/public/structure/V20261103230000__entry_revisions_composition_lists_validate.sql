-- Validates the check V20261023090800 added NOT VALID. The archive re-derivation (T1-C2) ran on
-- prod on 2026-11-03, and the inventory check 'entry_revision_malformed_composition_lists' found
-- no rows there. VALIDATE takes SHARE UPDATE EXCLUSIVE on entry_revisions, so the API keeps
-- reading and writing while it scans the table once, and it writes nothing. If a row breaks the
-- check, the migration fails and applies nothing; the inventory check lists that row.
-- lock_timeout makes it fail cleanly instead of queueing behind other DDL on the table.
SET LOCAL lock_timeout = '5s';

ALTER TABLE entry_revisions VALIDATE CONSTRAINT entry_revisions_composition_lists_chk;
