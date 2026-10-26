-- "One metric per key on a model revision" cannot be a plain unique constraint: the key
-- lives on metrics, not on the link table, and existing data may contain duplicates that a
-- migration must not resolve by deleting or choosing a value.
--
-- Plan (four migrations, each short on locks):
--   090300  add model_revision_metrics.metric_key (nullable, no default: metadata only) and a
--           trigger that fills it for every new link.
--   090400  backfill existing links in committed batches, quarantining duplicates.
--   090500  unique index over (model_revision_id, metric_key), built CONCURRENTLY.
--
-- metric_key is a trigger-maintained copy of metrics.key. A link inserted with an explicit
-- metric_key keeps it: the revision-copy path passes the source value so a quarantined
-- duplicate stays quarantined (see 090400) instead of colliding on the unique index.
--
-- ADD COLUMN and CREATE TRIGGER take locks that block writers (ACCESS EXCLUSIVE and SHARE ROW
-- EXCLUSIVE) for milliseconds; lock_timeout makes the migration fail cleanly (nothing is
-- applied, rerun it) instead of queueing behind a long transaction and blocking all traffic.
SET LOCAL lock_timeout = '5s';

ALTER TABLE model_revision_metrics
    ADD COLUMN metric_key text;

CREATE FUNCTION model_revision_metrics_set_metric_key() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.metric_key IS NOT NULL THEN
        RETURN NEW;
    END IF;
    SELECT metrics.key INTO NEW.metric_key FROM metrics WHERE metrics.id = NEW.metric_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER model_revision_metrics_set_metric_key
    BEFORE INSERT OR UPDATE OF metric_id ON model_revision_metrics
    FOR EACH ROW EXECUTE FUNCTION model_revision_metrics_set_metric_key();
