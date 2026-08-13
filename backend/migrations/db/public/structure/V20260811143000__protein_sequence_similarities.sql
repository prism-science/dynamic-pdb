CREATE TABLE protein_sequence_similarity_runs (
    id                   uuid PRIMARY KEY,
    tool                 text NOT NULL,
    parameters           jsonb NOT NULL DEFAULT '{}',
    state                text NOT NULL,
    error_message        text,
    started_at           timestamptz,
    finished_at          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE protein_sequence_similarities (
    id                  uuid PRIMARY KEY,
    run_id              uuid NOT NULL REFERENCES protein_sequence_similarity_runs(id) ON DELETE CASCADE,
    source_sequence_id  uuid NOT NULL REFERENCES protein_sequences(id),
    similar_sequence_id uuid NOT NULL REFERENCES protein_sequences(id),
    tool                text NOT NULL,
    score               double precision NOT NULL,
    metadata            jsonb NOT NULL DEFAULT '{}',
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX protein_sequence_similarities_pair_tool_idx
    ON protein_sequence_similarities(source_sequence_id, similar_sequence_id, tool);

CREATE INDEX protein_sequence_similarities_source_score_idx
    ON protein_sequence_similarities(source_sequence_id, score DESC);

CREATE INDEX protein_sequence_similarity_runs_active_created_at_idx
    ON protein_sequence_similarity_runs(created_at)
    WHERE state NOT IN ('succeeded', 'failed');
