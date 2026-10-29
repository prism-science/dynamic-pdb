-- Gives the rows the checksum job already checked their copy status (OPS-C). Only verified and
-- mismatch rows change, about 1,900 in production; pending and failed rows are not written.
-- The previous migration's CHECK applies to every row written here.
--
-- A file in our bucket has a URI in its artifact's own folder, ".../artifacts/<artifact id>/...",
-- the folder the backend requires of every stored file. Such a row gets own_storage, also when
-- its bytes differ from the declared values, since it never needed a copy.
--
-- A verified external file without a copy is over the copy limit or its copy failed, and the
-- reason was not stored. Those rows (a few hundred in production) go back to pending as if never
-- checked, and the job's next run reads them again and records the reason (user decision
-- 2026-10-29).
--
-- UPDATE takes only row locks, so the API keeps reading and writing the table. lock_timeout
-- keeps the migration from queueing behind a long transaction.
SET LOCAL lock_timeout = '5s';

UPDATE artifact_checksums
SET copy_status = 'copied'
WHERE status = 'verified'
  AND stored_uri IS NOT NULL;

UPDATE artifact_checksums checksum
SET copy_status = 'own_storage'
FROM artifacts artifact
WHERE artifact.id = checksum.artifact_id
  AND checksum.status IN ('verified', 'mismatch')
  AND checksum.stored_uri IS NULL
  AND strpos(artifact.uri, '/artifacts/' || artifact.id::text || '/') > 0;

UPDATE artifact_checksums
SET copy_status = 'mismatch'
WHERE status = 'mismatch'
  AND copy_status IS NULL;

UPDATE artifact_checksums
SET status = 'pending',
    sha256 = NULL,
    size_bytes = NULL,
    last_error = NULL,
    checked_at = NULL,
    attempts = 0,
    next_attempt_at = now()
WHERE status = 'verified'
  AND copy_status IS NULL;
