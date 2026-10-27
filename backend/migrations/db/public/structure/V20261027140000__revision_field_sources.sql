-- Entry and model revisions record where the values of their archive-synced fields came from,
-- in field_sources: a JSON object keyed by JSON pointer (for example /title or /metrics/r_free),
-- each value {source, accession, retrieved_at}. Source depositor marks a value a user set or
-- changed; the archive sync never overwrites it.
--
-- Existing revisions keep NULL. They were imported from the archive, so none of their fields
-- count as typed by a depositor, and the sync may keep updating them. No backfill is needed.
--
-- Both columns are nullable with no default, so ADD COLUMN changes only the catalog. It takes
-- ACCESS EXCLUSIVE on each table for milliseconds; lock_timeout makes the migration fail
-- cleanly (nothing applied, rerun it) instead of queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

ALTER TABLE entry_revisions ADD COLUMN field_sources jsonb;
ALTER TABLE model_revisions ADD COLUMN field_sources jsonb;
