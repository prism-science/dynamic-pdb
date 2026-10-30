-- What the model-compounds job read from each model file in our bucket: every copy of every
-- compound the file models outside its polymer chains, water excepted, with no reference-list
-- rule applied (cif.ReadModeledCompounds, T3-M). The entry's Ligands and Cofactors lists keep
-- the judgments; these rows record the coordinates.
--
-- One set per artifact, not per model revision: a revision that keeps its file keeps its
-- artifact, so a metadata-only edit reuses the set without reading the file again.
--
-- status: pending (not read yet, or waiting for a retry), done (read; the records are the
-- copies), failed (the file could not be read after the job's last attempt; last_error says
-- why). complete is false when a limit stopped the list; warnings are the reader's.
--
-- next_attempt_at is when the job may take a pending set. A claim moves it ahead by the lease,
-- so a set held by a job that crashed becomes due again by itself.
--
-- New model artifacts in our bucket get a pending set when they are created; the next
-- migration gives one to the existing ones.
--
-- The foreign keys take SHARE ROW EXCLUSIVE on artifacts and runs for milliseconds;
-- lock_timeout makes the migration fail cleanly (nothing applied, rerun it) instead of
-- queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE modeled_compound_sets (
    artifact_id      uuid PRIMARY KEY REFERENCES artifacts(id),
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'done', 'failed')),
    -- The run names the parser and its version (cif.ParserName, cif.ParserVersion).
    run_id           uuid REFERENCES runs(id),
    complete         boolean,
    warnings         jsonb NOT NULL DEFAULT '[]',
    attempts         integer NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    last_error       text,
    read_at          timestamptz,
    CONSTRAINT modeled_compound_sets_done_check CHECK (
        status <> 'done'
        OR (run_id IS NOT NULL AND complete IS NOT NULL AND read_at IS NOT NULL)
    )
);

-- The next two migrations add its indexes.

-- One row per copy, in the reader's order (CCD ID, chain, residue number). The chain, residue
-- number and insertion code are the ones viewers show; label_asym_id and entity_id are empty
-- in a PDB-format file.
CREATE TABLE modeled_compound_records (
    artifact_id    uuid NOT NULL REFERENCES modeled_compound_sets(artifact_id),
    ordinal        integer NOT NULL,
    ccd_id         text NOT NULL,
    label_asym_id  text,
    auth_asym_id   text,
    auth_seq_id    text,
    pdb_ins_code   text,
    entity_id      text,
    source         text NOT NULL
                   CHECK (source IN ('nonpoly_scheme', 'branch_scheme', 'atom_site', 'pdb_hetatm')),
    needs_review   boolean NOT NULL DEFAULT false,
    review_reason  text,
    PRIMARY KEY (artifact_id, ordinal),
    CONSTRAINT modeled_compound_records_review_check CHECK (needs_review = (review_reason IS NOT NULL))
);
