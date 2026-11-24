import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import path from "node:path";

import { stack } from "./stack";

// Writes to the browser stack's database what only a background job writes, and
// no API can: the model-compounds job's read of a model file. It goes through the
// compose project that `make browser-stack` started, the same way that target
// loads e2e/stack/seed.sql (set COMPOSE_PROJECT_NAME for a stack under another
// project name).

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
