import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import path from "node:path";

import { stack } from "./stack";

// Writes to the browser stack's database what no API can: the model-compounds
// job's read of a model file, the checksum job's check of a stored file, a review
// flag the archive sync opens, and a model approved before the grouping rule. It
// goes through the compose project that `make browser-stack` started, the same
// way that target loads e2e/stack/seed.sql (set COMPOSE_PROJECT_NAME for a
// stack under another project name).

const stackDirectory = path.join(__dirname, "../stack");

/** docker-compose when installed, else the docker compose plugin, as the website Makefile chooses. */
function composeCommand(): [string, ...string[]] {
  try {
    execFileSync("docker-compose", ["version"], { stdio: "ignore" });
    return ["docker-compose"];
  } catch {
    return ["docker", "compose"];
  }
}

function runSql(sql: string): void {
  const [command, ...prefix] = composeCommand();
  execFileSync(
    command,
    [
      ...prefix,
      "--env-file",
      path.join(stackDirectory, "stack.env"),
      "-f",
      path.join(stackDirectory, "compose.yml"),
      "exec",
      "-T",
      "postgres",
      "psql",
      "-U",
      stack.dbUser,
      "-d",
      stack.dbName,
      "-v",
      "ON_ERROR_STOP=1",
      "-q",
    ],
    { input: sql, stdio: ["pipe", "ignore", "inherit"] },
  );
}

/** The system user the background jobs write as (models.SystemUserID). */
const SYSTEM_USER_ID = "00000000-0000-0000-0000-000000000001";
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const CCD_ID = /^[A-Z0-9]{1,5}$/;

/** One copy of a compound in a model file; a review reason marks it "needs review". */
export type CompoundCopy = { ccdId: string; reviewReason?: string };

/**
 * Records that the model-compounds job has read a model file and found these
 * copies, as db.SaveModeledCompounds does: a parser run, the copies, and the
 * file's set marked done.
 */
export function saveModelFileCompounds(modelFileId: string, copies: readonly CompoundCopy[]): void {
  if (!UUID.test(modelFileId)) {
    throw new Error(`not a model file id: ${modelFileId}`);
  }
  for (const copy of copies) {
    if (!CCD_ID.test(copy.ccdId) || copy.reviewReason?.includes("'")) {
      throw new Error(`not a compound copy this helper writes: ${JSON.stringify(copy)}`);
    }
  }
  const runId = randomUUID();
  const records = copies.map((copy, index) => {
    const reason = copy.reviewReason === undefined ? "null" : `'${copy.reviewReason}'`;
    const needsReview = copy.reviewReason === undefined ? "false" : "true";
    return `('${modelFileId}', ${index}, '${copy.ccdId}', 'A', '${index + 1}', 'pdb_hetatm', ${needsReview}, ${reason})`;
  });
  runSql(`
    begin;
    insert into runs (id, name, software_name, software_version, created_by, started_at, finished_at)
    values ('${runId}', 'modeled compound read', 'browser test', '1', '${SYSTEM_USER_ID}', now(), now());
    insert into modeled_compound_sets (artifact_id, status, run_id, complete, read_at)
    values ('${modelFileId}', 'done', '${runId}', true, now())
    on conflict (artifact_id) do update
      set status = 'done', run_id = excluded.run_id, complete = true, read_at = excluded.read_at, last_error = null;
    ${
      records.length > 0
        ? `insert into modeled_compound_records
             (artifact_id, ordinal, ccd_id, auth_asym_id, auth_seq_id, source, needs_review, review_reason)
           values ${records.join(", ")};`
        : ""
    }
    commit;
  `);
}

const ISSUE_CODE = /^[a-z_]{1,64}$/;
const OBJECT_ID = /^[A-Za-z0-9_-]{1,64}$/;

/**
 * Opens a review flag on a record, as the archive sync or a migration does
 * (db.MigrationReviews.Open): a schema_migration_review row without a
 * resolution.
 */
export function openReviewFlag(objectType: "model" | "entry", objectId: string, issue: string): void {
  if (!OBJECT_ID.test(objectId) || !ISSUE_CODE.test(issue)) {
    throw new Error(`not a review flag this helper writes: ${objectType} ${objectId} ${issue}`);
  }
  runSql(`
    insert into schema_migration_review (object_type, object_id, issue)
    values ('${objectType}', '${objectId}', '${issue}')
    on conflict (object_type, object_id, issue) do update set resolved_at = null, resolved_by = null;
  `);
}

/**
 * Makes an in-review model revision active the way an approval did before the
 * grouping rule (researcher answer of 17 Nov, Q11), as db.ModelsRepository
 * ActivateRevision does, and gives an archive model its data sync job, as
 * ensureArchiveModelSyncJob does: the API now refuses to approve a PDB
 * deposition filed under another entry, but the production data holds such
 * models from before. The entry's search index is not rebuilt here; the next
 * approval of any model of the entry rebuilds it.
 */
export function activateModelFiledBeforeTheRule(revisionId: string): void {
  if (!UUID.test(revisionId)) {
    throw new Error(`not a model revision id: ${revisionId}`);
  }
  runSql(`
    begin;
    update model_revisions set state = 'archived', updated_at = now()
    where model_id = (select model_id from model_revisions where id = '${revisionId}') and state = 'active';
    update model_revisions
    set state = 'active', published_at = now(), updated_at = now(),
        revision_number = coalesce(
          (select max(previous.revision_number) from model_revisions previous
           where previous.model_id = model_revisions.model_id), 0) + 1
    where id = '${revisionId}' and state = 'in_review';
    update models set state = model_revisions.model_state
    from model_revisions where model_revisions.id = '${revisionId}' and models.id = model_revisions.model_id;
    insert into data_sync_jobs (entry_id, model_id, scheduled_at)
    select models.entry_id, models.id, now() + interval '7 days'
    from model_revisions join models on models.id = model_revisions.model_id
    where model_revisions.id = '${revisionId}'
      and model_revisions.metadata #>> '{external_refs,pdb}' is not null
      and (model_revisions.metadata ->> 'origin' = 'archive' or model_revisions.metadata ->> 'model_type' = 'Deposited')
    on conflict on constraint data_sync_jobs_entry_model_key do nothing;
    commit;
  `);
}

const SHA256 = /^[0-9a-f]{64}$/;

/**
 * Records that the checksum job read an uploaded file in our storage and found
 * the recorded SHA-256, as it does for own uploads: the archive worker includes
 * only files checked this way.
 */
export function markStoredFileVerified(artifactId: string, sha256: string, sizeBytes: number): void {
  if (!UUID.test(artifactId) || !SHA256.test(sha256) || !Number.isInteger(sizeBytes) || sizeBytes < 0) {
    throw new Error(`not a stored file this helper marks: ${artifactId} ${sha256} ${sizeBytes}`);
  }
  runSql(`
    insert into artifact_checksums (artifact_id, status, sha256, size_bytes, copy_status, checked_at)
    values ('${artifactId}', 'verified', '${sha256}', ${sizeBytes}, 'own_storage', now())
    on conflict (artifact_id) do update
      set status = 'verified', sha256 = excluded.sha256, size_bytes = excluded.size_bytes,
          copy_status = 'own_storage', checked_at = excluded.checked_at;
  `);
}
