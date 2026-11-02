import { request, type APIRequestContext, type APIResponse } from "@playwright/test";

import { mintToken, type TestUser } from "./auth";
import { stack } from "./stack";

const jsonApiMediaType = "application/vnd.api+json";

export type RevisionState = "in_review" | "active" | "rejected";

type CreateEntryResult = {
  data: { attributes: { entry_id: string; revision_id: string; state: RevisionState } };
};

type RevisionGroups = {
  data: {
    id: string;
    attributes: {
      entry: { title: string };
      entry_revisions: { id: string; state: RevisionState; parent_revision_id: string | null }[];
      model_revisions: { id: string; model_id: string; state: RevisionState }[];
    };
  }[];
};

/** One entry's revisions that wait for review, as GET /v1/users/{id}/entries/revisions lists them. */
export type InReviewGroup = {
  entryId: string;
  /** The entry's current title: the active one, or the new entry's own. */
  title: string;
  entryRevisions: RevisionGroups["data"][number]["attributes"]["entry_revisions"];
  modelRevisions: RevisionGroups["data"][number]["attributes"]["model_revisions"];
};

export type EntryRevision = {
  id: string;
  state: RevisionState;
  parent_revision_id: string | null;
  title: string;
  method: string | null;
  details?: string | null;
  resolution: number | null;
};

export type ModelRevision = {
  id: string;
  state: RevisionState;
  title: string;
  artifacts: { id: string; name: string; type: string; uri: string; sha256: string | null }[];
};

/**
 * The backend's JSON:API, called directly as one user. Tests use it to set up
 * records and to check what a form saved, never for the flow under test.
 */
export class BackendClient {
  private constructor(
    private readonly context: APIRequestContext,
    readonly user: TestUser,
  ) {}

  static async signedInAs(user: TestUser): Promise<BackendClient> {
    const context = await request.newContext({
      baseURL: stack.backendURL,
      extraHTTPHeaders: {
        Accept: jsonApiMediaType,
        Authorization: `Bearer ${mintToken(user)}`,
      },
    });
    return new BackendClient(context, user);
  }

  async dispose(): Promise<void> {
    await this.context.dispose();
  }

  /** POSTs a JSON:API body. The backend answers 415 to any other content type. */
  post(path: string, body: unknown): Promise<APIResponse> {
    return this.context.post(path, {
      data: body,
      headers: { "Content-Type": jsonApiMediaType },
    });
  }

  async createEntry(entry: { title: string; method: string; details: string }) {
    const response = await this.post("/v1/entries", {
      entry: { ...entry, artifacts: [] },
      model_operations: [],
    });
    const body = await expectStatus<CreateEntryResult>(response, 201, "create entry");
    return {
      entryId: body.data.attributes.entry_id,
      revisionId: body.data.attributes.revision_id,
    };
  }

  /** Publishes another user's revision. The backend refuses a reviewer's own (SELF_REVIEW). */
  async approveEntryRevision(entryId: string, revisionId: string): Promise<void> {
    const response = await this.context.patch(
      `/v1/entries/${entryId}/revisions/${revisionId}`,
      { data: { state: "active" }, headers: { "Content-Type": jsonApiMediaType } },
    );
    await expectStatus(response, 200, "approve entry revision");
  }

  /** This user's revisions that wait for review, grouped by entry. */
  async inReviewGroups(): Promise<InReviewGroup[]> {
    const response = await this.context.get(
      `/v1/users/${this.user.id}/entries/revisions?state=in_review`,
    );
    const body = await expectStatus<RevisionGroups>(response, 200, "list in-review revisions");
    return body.data.map((group) => ({
      entryId: group.id,
      title: group.attributes.entry.title,
      entryRevisions: group.attributes.entry_revisions,
      modelRevisions: group.attributes.model_revisions,
    }));
  }

  /** The in-review group of one entry; fails when the entry has nothing in review. */
  async inReviewGroup(match: { entryId: string } | { title: string }): Promise<InReviewGroup> {
    const groups = await this.inReviewGroups();
    const group = groups.find((item) =>
      "entryId" in match ? item.entryId === match.entryId : item.title === match.title,
    );
    if (!group) {
      throw new Error(`no in-review revisions for ${JSON.stringify(match)}`);
    }
    return group;
  }

  async entryRevision(entryId: string, revisionId: string): Promise<EntryRevision> {
    const response = await this.context.get(
      `/v1/users/${this.user.id}/entries/${entryId}/revisions/${revisionId}`,
    );
    const body = await expectStatus<{ data: { attributes: EntryRevision } }>(
      response,
      200,
      "get entry revision",
    );
    return body.data.attributes;
  }

  async modelRevision(entryId: string, modelId: string, revisionId: string): Promise<ModelRevision> {
    const response = await this.context.get(
      `/v1/users/${this.user.id}/entries/${entryId}/models/${modelId}/revisions/${revisionId}`,
    );
    const body = await expectStatus<{ data: { attributes: ModelRevision } }>(
      response,
      200,
      "get model revision",
    );
    return body.data.attributes;
  }
}

/**
 * An entry the submitter deposited and the reviewer approved, which is what the
 * edit and add-model pages need: they show active entries only.
 */
export async function createActiveEntry(
  submitter: BackendClient,
  reviewer: BackendClient,
  title: string,
): Promise<string> {
  const { entryId, revisionId } = await submitter.createEntry({
    title,
    method: "X-ray crystallography",
    details: "Created by the browser tests.",
  });
  await reviewer.approveEntryRevision(entryId, revisionId);
  return entryId;
}

export async function expectStatus<Body = unknown>(
  response: APIResponse,
  status: number,
  what: string,
): Promise<Body> {
  if (response.status() !== status) {
    throw new Error(
      `${what}: expected HTTP ${status}, got ${response.status()}: ${await response.text()}`,
    );
  }
  return (await response.json()) as Body;
}
