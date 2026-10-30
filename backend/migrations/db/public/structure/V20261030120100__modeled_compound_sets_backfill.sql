-- Gives every existing model file in our bucket a pending set, so the model-compounds job reads
-- it (T3-M). New ones get theirs when the artifact is created.
--
-- A file in our bucket has a URI in its artifact's own folder, ".../artifacts/<artifact id>/...",
-- the folder the backend requires of every stored file (the same test as the OPS-C backfill).
-- Archive models point at files.rcsb.org and get no set; T3-M2 covers them. In production this
-- adds a few dozen rows. The scan of artifacts reads every row once (about 570,000).
--
-- INSERT takes only row locks on artifacts (FOR KEY SHARE, for the foreign key) of the rows it
-- adds. lock_timeout keeps the migration from queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

INSERT INTO modeled_compound_sets (artifact_id)
SELECT artifact.id
FROM artifacts artifact
WHERE artifact.type = 'model'
  AND strpos(artifact.uri, '/artifacts/' || artifact.id::text || '/') > 0
ON CONFLICT (artifact_id) DO NOTHING;
