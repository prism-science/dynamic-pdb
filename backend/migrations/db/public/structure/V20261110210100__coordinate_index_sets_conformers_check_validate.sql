-- Validates coordinate_index_sets_conformers_check, which V20261110210000 added NOT VALID. VALIDATE
-- takes SHARE UPDATE EXCLUSIVE on coordinate_index_sets: the job and the API keep reading and
-- writing while it scans the table once (about 225,000 rows after S2's fill), and it writes nothing.
-- Every existing row has the new columns null, which the check allows. lock_timeout makes it fail
-- cleanly instead of queueing behind other DDL on the table.
SET LOCAL lock_timeout = '5s';

ALTER TABLE coordinate_index_sets VALIDATE CONSTRAINT coordinate_index_sets_conformers_check;
