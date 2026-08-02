ALTER TABLE entries
    RENAME TO structures;

ALTER TABLE entry_search_index
    RENAME TO structure_search_index;

ALTER TABLE models
    RENAME COLUMN entry_id TO structure_id;

ALTER TABLE entities
    RENAME COLUMN entry_id TO structure_id;

ALTER TABLE structure_search_index
    RENAME COLUMN entry_id TO structure_id;

UPDATE structure_search_index
SET model_type = 'structure'
WHERE model_type = 'entry';

ALTER TABLE structures
    RENAME CONSTRAINT entries_pkey TO structures_pkey;

ALTER TABLE structures
    RENAME CONSTRAINT entries_created_by_fkey TO structures_created_by_fkey;

ALTER TABLE models
    RENAME CONSTRAINT models_entry_id_fkey TO models_structure_id_fkey;

ALTER TABLE entities
    RENAME CONSTRAINT entities_entry_id_fkey TO entities_structure_id_fkey;

ALTER TABLE structure_search_index
    RENAME CONSTRAINT entry_search_index_entry_id_fkey TO structure_search_index_structure_id_fkey;

ALTER TABLE structure_search_index
    RENAME CONSTRAINT entry_search_index_pkey TO structure_search_index_pkey;

ALTER INDEX entries_created_by_idx
    RENAME TO structures_created_by_idx;

ALTER INDEX models_entry_id_idx
    RENAME TO models_structure_id_idx;

ALTER INDEX entities_entry_id_idx
    RENAME TO entities_structure_id_idx;

ALTER INDEX entry_search_index_search_tsv_idx
    RENAME TO structure_search_index_search_tsv_idx;
