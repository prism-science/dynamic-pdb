-- CONCURRENTLY: takes only SHARE UPDATE EXCLUSIVE, so reads and writes continue. A superset of
-- the old primary key, so it cannot fail on existing data. If it is interrupted it leaves an
-- INVALID index and Flyway has the migration recorded as failed:
-- `DROP INDEX CONCURRENTLY IF EXISTS run_artifacts_run_artifact_direction_idx;`, `flyway repair`,
-- then migrate again.
CREATE UNIQUE INDEX CONCURRENTLY run_artifacts_run_artifact_direction_idx
    ON run_artifacts(run_id, artifact_id, direction);
