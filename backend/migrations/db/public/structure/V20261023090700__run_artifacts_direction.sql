-- run_artifacts: one artifact may be both input and output of a run. The unique index was
-- built concurrently in the previous migration; swapping the primary key onto it is
-- metadata-only (no scan, the columns are already NOT NULL). position is the artifact's order
-- within the run, unknown when NULL.
--
-- Both checks are NOT VALID (no scan; existing rows are not checked). The application only
-- writes 'input' and 'output' (attachArtifactToRun validates the direction), and nothing
-- writes position yet.
--
-- ACCESS EXCLUSIVE on run_artifacts for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE run_artifacts
    DROP CONSTRAINT run_artifacts_pkey,
    ADD CONSTRAINT run_artifacts_pkey PRIMARY KEY USING INDEX run_artifacts_run_artifact_direction_idx,
    ADD COLUMN position integer,
    ADD CONSTRAINT run_artifacts_direction_chk CHECK (direction IN ('input', 'output')) NOT VALID,
    ADD CONSTRAINT run_artifacts_position_chk CHECK (position IS NULL OR position >= 0) NOT VALID;
