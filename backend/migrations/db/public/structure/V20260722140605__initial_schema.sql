CREATE TABLE users (
    id            uuid PRIMARY KEY,

    source        text NOT NULL,
    external_ref  text NOT NULL,

    email         text,
    display_name  text,
    avatar_url    text,

    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,

    UNIQUE (source, external_ref)
);

CREATE TABLE entries (
    id                  uuid PRIMARY KEY,
    name                text NOT NULL,
    description         text,
    thumbnail_image_url text,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE models (
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
    model_id uuid
        REFERENCES models(id) ON DELETE CASCADE,

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

CREATE TABLE entry_search_index (
    entry_id    uuid NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    model_type  text NOT NULL,
    model_id    text NOT NULL,
    updated_at  timestamptz NOT NULL,
    search_text text NOT NULL,
    search_tsv  tsvector NOT NULL,

    PRIMARY KEY (entry_id, model_type, model_id)
);

CREATE INDEX models_entry_id_idx
    ON models(entry_id);

CREATE INDEX entities_entry_id_idx
    ON entities(entry_id);

CREATE INDEX entities_model_id_idx
    ON entities(model_id);

CREATE INDEX entity_relations_source_idx
    ON entity_relations(source_entity_id);

CREATE INDEX entity_relations_target_idx
    ON entity_relations(target_entity_id);

CREATE INDEX entry_search_index_search_tsv_idx
    ON entry_search_index
    USING GIN (search_tsv);
