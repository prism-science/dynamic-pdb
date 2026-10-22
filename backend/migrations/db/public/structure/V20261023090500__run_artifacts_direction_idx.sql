CREATE UNIQUE INDEX CONCURRENTLY run_artifacts_run_artifact_direction_idx
    ON run_artifacts(run_id, artifact_id, direction);
