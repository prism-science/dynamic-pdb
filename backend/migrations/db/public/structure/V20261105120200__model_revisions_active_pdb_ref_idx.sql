-- Finds the active models naming a PDB ID: the public pdb_id filter and the check that one PDB ID
-- belongs to one entry.
CREATE INDEX CONCURRENTLY model_revisions_active_pdb_ref_idx
    ON model_revisions ((upper(metadata #>> '{external_refs,pdb}')))
    WHERE state = 'active'
      AND metadata #>> '{external_refs,pdb}' IS NOT NULL;
