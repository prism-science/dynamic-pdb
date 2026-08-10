CREATE UNIQUE INDEX entry_revisions_active_pdb_ref_idx
    ON entry_revisions ((upper(metadata #>> '{external_refs,pdb}')))
    WHERE state = 'active'
      AND metadata #>> '{external_refs,pdb}' IS NOT NULL;
