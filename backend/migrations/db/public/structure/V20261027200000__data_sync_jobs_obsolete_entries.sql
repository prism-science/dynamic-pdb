-- V20261027150000 gave every active archive entry without an entry job a job that was due at
-- once, so that its first run would label the entry obsolete at RCSB. In production all 28 of
-- those jobs were lost: Argo CD runs migrations before it replaces the pods, so the old backend
-- was still running. It picked up the due jobs, got 404 from RCSB and deleted them, as it always
-- did for a missing entry. The entries are still unlabelled and have no job.
--
-- Give them their job again. The backend running now already labels obsolete entries and stops
-- their jobs, so the 15-minute delay is only a safety margin: it leaves the job to the pods that
-- start after this migration. Like V20261027150000, the insert skips any entry that already has
-- an entry job, stopped or not, so rerunning it adds nothing. It takes row locks only.
INSERT INTO data_sync_jobs(entry_id, model_id, scheduled_at)
SELECT revision.entry_id, NULL, now() + interval '15 minutes'
FROM entry_revisions revision
JOIN entries entry ON entry.id = revision.entry_id
WHERE revision.state = 'active'
  AND entry.state = 'active'
  AND revision.metadata -> 'external_refs' ->> 'pdb' IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM data_sync_jobs job
      WHERE job.entry_id = revision.entry_id
        AND job.model_id IS NULL
  );
