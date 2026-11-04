-- T1-E: the database refuses a metric key outside Dynamic PDB Schema v0.2.0, the same list as
-- models.MetricKey.Validate. Both envs validate in enforce mode, so the API and the data sync
-- already refuse these keys; this check also stops any other writer.
--
-- NOT VALID: no scan, existing rows are not checked here (V20261104150400 validates them). A NOT
-- VALID check still applies to an UPDATE of an existing row, so the inventory check
-- 'metric_unknown_key' must be empty before this ships (prod: 0 rows on 2026-11-04).
--
-- ACCESS EXCLUSIVE on metrics for milliseconds; lock_timeout makes the migration fail cleanly
-- (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE metrics
    ADD CONSTRAINT metrics_key_known_chk CHECK (key IN (
        'r_work', 'r_free', 'cc', 'rscc', 'clashscore', 'ramachandran_outliers',
        'side_chain_outliers', 'molprobity_score', 'wilson_b', 'rmsf')) NOT VALID;
