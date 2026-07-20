CREATE TABLE entries (
    id                  uuid PRIMARY KEY,
    name                text NOT NULL,
    description         text,
    thumbnail_image_url text,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE experiments (
    id                  uuid PRIMARY KEY,
    entry_id            uuid NOT NULL
        REFERENCES entries(id) ON DELETE CASCADE,

    name                text NOT NULL,
    description         text,
    thumbnail_image_url text,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE entities (
    id            uuid PRIMARY KEY,
    entry_id      uuid NOT NULL
        REFERENCES entries(id) ON DELETE CASCADE,
    experiment_id uuid
        REFERENCES experiments(id) ON DELETE CASCADE,

    type          text NOT NULL,
    level         text,
    name          text NOT NULL,
    payload       jsonb,

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT entities_level_check
        CHECK (level IS NULL OR level IN ('L0', 'L1', 'L2', 'L3'))
);

CREATE TABLE entity_relations (
    id               uuid PRIMARY KEY,

    source_entity_id uuid NOT NULL
        REFERENCES entities(id) ON DELETE CASCADE,

    target_entity_id uuid NOT NULL
        REFERENCES entities(id) ON DELETE CASCADE,

    relation_type    text NOT NULL,

    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT entity_relations_no_self_reference
        CHECK (source_entity_id <> target_entity_id),

    CONSTRAINT entity_relations_unique
        UNIQUE (source_entity_id, target_entity_id, relation_type)
);

CREATE INDEX experiments_entry_id_idx
    ON experiments(entry_id);

CREATE INDEX entities_entry_id_idx
    ON entities(entry_id);

CREATE INDEX entities_experiment_id_idx
    ON entities(experiment_id);

CREATE INDEX entity_relations_source_idx
    ON entity_relations(source_entity_id);

CREATE INDEX entity_relations_target_idx
    ON entity_relations(target_entity_id);
