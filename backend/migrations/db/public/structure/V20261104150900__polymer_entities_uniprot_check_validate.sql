-- Validates polymer_entities_uniprot_mappings_chk, which V20261104150300 added NOT VALID. VALIDATE takes
-- SHARE UPDATE EXCLUSIVE on polymer_entities: the API and the jobs keep reading and writing while it
-- scans the table once, and it writes nothing. One statement per migration, so each runs in
-- its own transaction. If a row breaks the check, the migration fails and applies nothing; the
-- check stays NOT VALID.
-- The inventory check 'polymer_entity_uniprot_mapping_invalid' lists such rows; on prod it listed
-- none on 2026-11-04.
-- lock_timeout makes it fail cleanly instead of queueing behind other DDL on the table.
SET LOCAL lock_timeout = '5s';

ALTER TABLE polymer_entities VALIDATE CONSTRAINT polymer_entities_uniprot_mappings_chk;
