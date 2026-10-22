-- Entry-level ligands and cofactors are either absent (unknown) or an array of strings ([] is
-- known to be empty). NOT VALID: no scan, existing rows are not checked.
--
-- It cannot reject a write the application makes today: the Go EntryMetadata omits the keys
-- when unset (pointer to slice with omitempty) and otherwise writes a list of strings, and
-- the only other writer, migration 090100, merges string lists only. The inventory check
-- 'entry_revision_malformed_composition_lists' must be empty before this ships; it lists
-- any row the check would reject on UPDATE.
--
-- ACCESS EXCLUSIVE on entry_revisions for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

-- strict mode: lax mode unwraps inner arrays, so [["x"]] would pass as all strings.
ALTER TABLE entry_revisions
    ADD CONSTRAINT entry_revisions_composition_lists_chk CHECK (
        (NOT metadata ? 'ligands'
         OR CASE WHEN jsonb_typeof(metadata -> 'ligands') = 'array'
                 THEN NOT jsonb_path_exists(metadata, 'strict $.ligands[*] ? (@.type() != "string")')
                 ELSE false END)
        AND (NOT metadata ? 'cofactors'
             OR CASE WHEN jsonb_typeof(metadata -> 'cofactors') = 'array'
                     THEN NOT jsonb_path_exists(metadata, 'strict $.cofactors[*] ? (@.type() != "string")')
                     ELSE false END)) NOT VALID;
