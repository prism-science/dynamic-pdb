import { randomUUID } from "node:crypto";

import { request, type APIRequestContext, type APIResponse } from "@playwright/test";

import { mintToken, type TestUser } from "./auth";
import { stack } from "./stack";

const jsonApiMediaType = "application/vnd.api+json";

export type RevisionState = "in_review" | "active" | "rejected";

type CreateModelResult = {
  data: { attributes: { entry_id: string; model_id: string; revision_id: string; state: RevisionState } };
};

/** A model the tests create through the API; see BackendClient.createModel. */
export type TestModel = {
  title: string;
  modelType: "Single Conformer" | "Multiconformer" | "Ensemble" | "Deposited";
  rFree: number;
};

export type CreatedModel = { modelId: string; revisionId: string };

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

/** Where one archive-synced field's value came from (FieldSource in openapi.yaml). */
export type FieldSource = {
  source: "rcsb" | "depositor" | "file";
  accession?: string;
  release?: string;
  retrieved_at?: string;
  /** For file values: the artifact the value was read from, and its checksum. */
  artifact_id?: string;
  sha256?: string;
};

export type EntryRevision = {
  id: string;
  state: RevisionState;
  parent_revision_id: string | null;
  title: string;
  method: string | null;
  details?: string | null;
  resolution: number | null;
  /** Keyed by JSON pointer, such as /title; null for revisions stored before sources. */
  field_sources?: Record<string, FieldSource> | null;
};

export type ModelRevision = {
  id: string;
  state: RevisionState;
  title: string;
  artifacts: { id: string; name: string; type: string; uri: string; sha256: string | null }[];
  metrics: {
    key: string;
    value: number;
    software_name?: string | null;
    software_version?: string | null;
    run_id?: string | null;
  }[];
  /** Where each archive-synced field's value came from, by JSON pointer. */
  field_sources: Record<
    string,
    { source: string; artifact_id?: string; sha256?: string; accession?: string }
  > | null;
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

  async createEntry(entry: {
    title: string;
    method: string;
    details?: string;
    resolution?: number;
    external_refs?: Record<string, string>;
  }) {
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

  /**
   * Adds a model to an entry through the API, with what enforce mode requires:
   * a coordinate file, the run that made it and a score that run computed.
   * The files are only records here: nothing reads their bytes.
   */
  async createModel(entryId: string, model: TestModel): Promise<CreatedModel> {
    const modelArtifactId = randomUUID();
    const inputArtifactId = randomUUID();
    const runId = randomUUID();
    const fileBase = `${stack.s3URL}/${stack.s3Bucket}/browser-tests/${modelArtifactId}`;
    const response = await this.post(`/v1/entries/${entryId}/models`, {
      model: {
        title: model.title,
        primary_artifact_id: modelArtifactId,
        metadata: { model_type: model.modelType, purpose: "Refinement" },
        artifacts: [
          {
            id: modelArtifactId,
            name: "model.pdb",
            level: "L2",
            type: "model",
            format: "pdb",
            uri: `${fileBase}/model.pdb`,
            sha256: "a".repeat(64),
            size_bytes: 100,
            metadata: {},
          },
          {
            id: inputArtifactId,
            name: "data.mtz",
            level: "L1",
            type: "structure_factors",
            format: "mtz",
            uri: `${fileBase}/data.mtz`,
            sha256: "b".repeat(64),
            size_bytes: 100,
            metadata: {},
          },
        ],
        metrics: [{ id: randomUUID(), key: "r_free", value: model.rFree, run_id: runId }],
        runs: [
          {
            id: runId,
            name: "refinement",
            software_name: "phenix.refine",
            software_version: "1.21",
            command: "phenix.refine",
            artifacts: [
              { artifact_id: inputArtifactId, direction: "input" },
              { artifact_id: modelArtifactId, direction: "output" },
            ],
          },
        ],
      },
    });
    const body = await expectStatus<CreateModelResult>(response, 201, "create model");
    return {
      modelId: body.data.attributes.model_id,
      revisionId: body.data.attributes.revision_id,
    };
  }

  /** Publishes another user's model revision. */
  async approveModelRevision(entryId: string, modelId: string, revisionId: string): Promise<void> {
    const response = await this.context.patch(
      `/v1/entries/${entryId}/models/${modelId}/revisions/${revisionId}`,
      { data: { state: "active" }, headers: { "Content-Type": jsonApiMediaType } },
    );
    await expectStatus(response, 200, "approve model revision");
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
  fields: { resolution?: number } = {},
): Promise<string> {
  const { entryId, revisionId } = await submitter.createEntry({
    title,
    method: "X-ray crystallography",
    details: "Created by the browser tests.",
    ...fields,
  });
  await reviewer.approveEntryRevision(entryId, revisionId);
  return entryId;
}

/** A model the submitter added and the reviewer published, so searches find it. */
export async function createActiveModel(
  submitter: BackendClient,
  reviewer: BackendClient,
  entryId: string,
  model: TestModel,
): Promise<string> {
  const { modelId, revisionId } = await submitter.createModel(entryId, model);
  await reviewer.approveModelRevision(entryId, modelId, revisionId);
  return modelId;
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
