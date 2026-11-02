import { randomUUID } from "node:crypto";

import { BackendClient, expectStatus } from "./support/api";
import { submitter } from "./support/auth";

type ErrorBody = { errors: { source?: { pointer?: string } }[] };

/**
 * Refuses to run the suite unless the backend rejects invalid requests, because
 * these tests exist to prove the forms save under `validation.mode: enforce`.
 *
 * A model with a title only breaks the provenance rules. The backend checks the
 * schema before it looks up the entry, so in enforce mode it answers 422 even for
 * an entry that does not exist; in log_only mode it would answer 404. Nothing is written.
 */
export default async function globalSetup(): Promise<void> {
  const api = await BackendClient.signedInAs(submitter);
  try {
    const response = await api.post(`/v1/entries/dpdb_missing_${randomUUID().slice(0, 8)}/models`, {
      model: { title: "Enforce-mode check", metadata: {}, artifacts: [] },
    });
    const body = await expectStatus<ErrorBody>(
      response,
      422,
      "the backend must run with validation.mode: enforce",
    );
    const pointers = body.errors.map((error) => error.source?.pointer);
    if (!pointers.includes("/model/runs")) {
      throw new Error(`expected a 422 at /model/runs, got ${JSON.stringify(body)}`);
    }
  } finally {
    await api.dispose();
  }
}
