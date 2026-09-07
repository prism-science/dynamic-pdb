CREATE TABLE polymer_entities (
    id                  uuid PRIMARY KEY,
    protein_sequence_id uuid NOT NULL REFERENCES protein_sequences(id),
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX polymer_entities_protein_sequence_id_idx
    ON polymer_entities(protein_sequence_id);

CREATE TABLE entry_revision_polymer_entities (
    entry_revision_id uuid NOT NULL REFERENCES entry_revisions(id),
    polymer_entity_id uuid NOT NULL REFERENCES polymer_entities(id),

    PRIMARY KEY (entry_revision_id, polymer_entity_id)
);

CREATE INDEX entry_revision_polymer_entities_polymer_entity_id_idx
    ON entry_revision_polymer_entities(polymer_entity_id);
