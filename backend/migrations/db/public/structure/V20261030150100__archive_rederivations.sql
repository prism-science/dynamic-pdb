-- What the archive re-derivation job (T1-C2) did to each active archive entry revision. The job
-- derives the entry lists, compound_sources and glycan_descriptors from the head of the entry's
-- RCSB mmCIF, and the UniProt-numbered mutations of the revision's polymer entities, the same way
-- the entry sync does. It changes the revision in place (no new revision), so before and after
-- keep the old and new values of what it changed, and
-- inventory/revert_archive_rederivation.sql puts before back.
--
-- outcome:
--   derived             the job changed the revision or its polymer entities.
--   unchanged           the derived values equal the stored ones.
--   skipped_person_set  a person set the lists, so they were kept; mutations may still have
--                       changed, and then before and after record them.
--   skipped_in_review   the entry has a revision in review; the next run tries again.
--   skipped_not_active  the revision stopped being active during the run; its successor gets
--                       its own row.
--   skipped_incomplete_file
--                       the head of the file could not be parsed or ends before the
--                       coordinates (error says which), so nothing was derived; the entry sync
--                       keeps the stored values for the same file.
--   failed              the file could not be downloaded or derived (error says why); the next
--                       run tries again until attempts reaches 3.
--
-- attempts counts the real runs that wrote the row.
--
-- dry_run rows record what a dry run would change; nothing else was written. A dry run writes
-- rows only for revisions that have none, and a real run replaces them.
--
-- The table starts empty. The foreign keys take SHARE ROW EXCLUSIVE on entry_revisions and
-- entries for milliseconds; lock_timeout makes the migration fail cleanly (nothing applied,
-- rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

CREATE TABLE archive_rederivations (
    entry_revision_id  uuid PRIMARY KEY REFERENCES entry_revisions(id),
    entry_id           text NOT NULL REFERENCES entries(id),
    pdb_id             text NOT NULL,
    outcome            text NOT NULL,
    dry_run            boolean NOT NULL DEFAULT false,
    reference_version  text,
    before             jsonb,
    after              jsonb,
    error              text,
    attempts           integer NOT NULL DEFAULT 1 CHECK (attempts >= 1),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT archive_rederivations_outcome_check CHECK (outcome IN (
        'derived', 'unchanged', 'skipped_person_set', 'skipped_in_review', 'skipped_not_active',
        'skipped_incomplete_file', 'failed'
    )),
    CONSTRAINT archive_rederivations_error_check CHECK (
        (outcome IN ('failed', 'skipped_incomplete_file')) = (error IS NOT NULL)
    ),
    -- A change has both sides; only derived and skipped_person_set rows record one, and a
    -- derived row always does.
    CONSTRAINT archive_rederivations_change_check CHECK (
        (before IS NULL) = (after IS NULL)
        AND (before IS NULL OR outcome IN ('derived', 'skipped_person_set'))
        AND (outcome <> 'derived' OR before IS NOT NULL)
    )
);
