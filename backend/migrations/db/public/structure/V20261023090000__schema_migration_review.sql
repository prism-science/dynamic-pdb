-- Records that the schema migrations could not resolve on their own. Migrations never
-- invent data or provenance: anything ambiguous or needing the archive lands here, and
-- the object itself stays public with its original values.
CREATE TABLE schema_migration_review (
    id          bigserial PRIMARY KEY,
    object_type text NOT NULL,
    object_id   text NOT NULL,
    issue       text NOT NULL,
    details     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (object_type, object_id, issue)
);
