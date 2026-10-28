-- 28 archive entries in production have no title: the archive import created them after RCSB had
-- obsoleted them. V20261027200000 gave them their entry job back, and its run marked them obsolete
-- and stopped the job. Their new revisions put them first in the entry list, as untitled rows.
--
-- The backend running after this migration takes the title of RCSB's removal record when it marks
-- an entry obsolete and the entry has none, and adds no revision when nothing changed. So run the
-- entry job of every active archive entry without a title once more. The 15-minute delay leaves
-- the jobs to the pods that start after this migration: the old backend would add another
-- untitled revision. Archive entries with a title keep their job as it is. It takes row locks only.
UPDATE data_sync_jobs job
SET stopped_at = NULL,
    stop_reason = NULL,
    scheduled_at = now() + interval '15 minutes'
FROM entry_revisions revision
JOIN entries entry ON entry.id = revision.entry_id
WHERE job.entry_id = revision.entry_id
  AND job.model_id IS NULL
  AND revision.state = 'active'
  AND entry.state = 'active'
  AND revision.metadata -> 'external_refs' ->> 'pdb' IS NOT NULL
  AND coalesce(btrim(revision.title), '') = '';
