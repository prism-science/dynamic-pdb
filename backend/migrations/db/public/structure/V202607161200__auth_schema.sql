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
