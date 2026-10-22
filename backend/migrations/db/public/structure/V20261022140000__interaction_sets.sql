CREATE TABLE interaction_sets (
    id                uuid PRIMARY KEY,
    model_revision_id uuid NOT NULL REFERENCES model_revisions(id),
    -- The run records which parser, and which parser version, produced the set.
    run_id            uuid NOT NULL REFERENCES runs(id),
    source            text NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT interaction_sets_source_check CHECK (source IN ('mmcif_struct_conn')),
    CONSTRAINT interaction_sets_model_revision_run_key UNIQUE (model_revision_id, run_id)
);

CREATE INDEX interaction_sets_run_id_idx
    ON interaction_sets(run_id);

CREATE TABLE interaction_records (
    id                    uuid PRIMARY KEY,
    interaction_set_id    uuid NOT NULL REFERENCES interaction_sets(id),
    ordinal               integer NOT NULL,

    -- The row of the source file: _struct_conn.id and _struct_conn.conn_type_id
    -- (metalc, covale, hydrog, disulf, ...), kept as written.
    source_connection_id  text NOT NULL,
    connection_type       text NOT NULL,

    a_label_asym_id       text,
    a_label_comp_id       text,
    a_label_seq_id        integer,
    a_label_atom_id       text,
    a_label_alt_id        text,
    a_auth_asym_id        text,
    a_auth_comp_id        text,
    a_auth_seq_id         text,
    a_pdb_ins_code        text,

    b_label_asym_id       text,
    b_label_comp_id       text,
    b_label_seq_id        integer,
    b_label_atom_id       text,
    b_label_alt_id        text,
    b_auth_asym_id        text,
    b_auth_comp_id        text,
    b_auth_seq_id         text,
    b_pdb_ins_code        text,

    a_symmetry            text,
    b_symmetry            text,
    distance_angstroms    double precision,
    value_order           text,

    CONSTRAINT interaction_records_set_connection_key UNIQUE (interaction_set_id, source_connection_id),
    CONSTRAINT interaction_records_set_ordinal_key UNIQUE (interaction_set_id, ordinal)
);
