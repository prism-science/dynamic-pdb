-- What the coordinate-index job read from each model file in our bucket (or our copy of an
-- archive file): one row per author chain of the file's first model (cif.ReadCoordinateIndex,
-- S2). The model search's chain filter reads them. S3 adds per-residue rows read in the same pass.
--
-- One set per artifact, not per model revision: a revision that keeps its file keeps its
-- artifact, so a metadata-only edit reuses the set without reading the file again.
--
-- status: pending (not read yet, or waiting for a retry), done (read; the chains are the file's),
-- failed (the file could not be read after the job's last attempt; last_error says why).
-- reader_version is cif.CoordinateIndexVersion of the read; warnings are the reader's.
--
-- fill marks a set the one-time fill queued; the job claims uploads (fill false) first, so a new
-- upload never waits behind the fill. next_attempt_at is when the job may take a pending set. A
-- claim moves it ahead by the lease, so a set held by a job that crashed becomes due again by
-- itself.
--
-- fasta_entry_ids are the entries of the models that used the file when it was read; a chain whose
-- length the file does not state takes it from the FASTA records of their active revisions. The
-- file is read again when the API changes those records, or when the entries of the models using
-- the file are no longer these (a file read before its model existed, a new model of another
-- entry, a model revision that switched to the file).
--
-- New model artifacts in our bucket get a pending set when they are created; the job's fill gives
-- one to the existing ones. No rows are written here.
--
-- The foreign key takes SHARE ROW EXCLUSIVE on artifacts for milliseconds; lock_timeout makes the
-- migration fail cleanly (nothing applied, rerun it) instead of queueing behind a long
-- transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE coordinate_index_sets (
    artifact_id       uuid PRIMARY KEY REFERENCES artifacts(id),
    status            text NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'done', 'failed')),
    fill              boolean NOT NULL DEFAULT false,
    reader_version    text,
    warnings          jsonb NOT NULL DEFAULT '[]',
    attempts          integer NOT NULL DEFAULT 0,
    next_attempt_at   timestamptz NOT NULL DEFAULT now(),
    last_error        text,
    read_at           timestamptz,
    fasta_entry_ids   text[] NOT NULL DEFAULT '{}',
    CONSTRAINT coordinate_index_sets_done_check CHECK (
        status <> 'done' OR (reader_version IS NOT NULL AND read_at IS NOT NULL)
    )
);

-- One row per author chain with polymer residues in the file's first model.
--   entity_id: the file's label_entity_id most of the chain's residues have; null when the file
--     states none (PDB format, most computed mmCIF files).
--   deposited_length: the chain's sequence length; length_source says where it came from: the
--     file's _entity_poly_seq or SEQRES, else the entry's FASTA record naming the chain as author
--     chain, else as label chain. Both null when no source gives one.
--   modeled_residues: polymer residues with coordinates (distinct residue number and insertion
--     code).
--   protein: a protein chain; protein_source says which test decided: entity_poly_type (the
--     _entity_poly.type of the chain's entity is polypeptide(L) or polypeptide(D)) or
--     residue_majority (the file states no polymer type for the chain, so more than half of its
--     modeled residues have a peptide CCD type).
CREATE TABLE coordinate_index_chains (
    artifact_id       uuid NOT NULL REFERENCES coordinate_index_sets(artifact_id),
    auth_asym_id      text NOT NULL,
    entity_id         text,
    deposited_length  integer CHECK (deposited_length > 0),
    length_source     text CHECK (length_source IN
                      ('entity_poly_seq', 'seqres', 'fasta_auth_chain', 'fasta_label_chain')),
    modeled_residues  integer NOT NULL CHECK (modeled_residues > 0),
    protein           boolean NOT NULL,
    protein_source    text NOT NULL CHECK (protein_source IN ('entity_poly_type', 'residue_majority')),
    PRIMARY KEY (artifact_id, auth_asym_id),
    CONSTRAINT coordinate_index_chains_length_known_check CHECK (
        (deposited_length IS NULL) = (length_source IS NULL)
    )
);

-- The next migrations add the indexes.
