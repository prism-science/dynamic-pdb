import "server-only";

import { createHash } from "node:crypto";

import { getApiBaseUrl, jsonApiMediaType } from "./baseUrl";
import { formatEntryLabel } from "@/lib/entry-label";
import { REVIEW_PAGE_SIZE } from "@/lib/reviewQueue";

type JSONRecord = Record<string, unknown>;

export type EntryCrystalGrowth = {
  ph?: number;
  temperature_kelvin?: number;
};

export type EntryDiffraction = {
  id: string;
  temperature_kelvin?: number;
};

export type EntryCrystal = {
  id: string;
  growth?: EntryCrystalGrowth;
  diffractions?: EntryDiffraction[];
};

export type EntryCrystallography = {
  crystals?: EntryCrystal[];
};

export type EntryProperties = {
  external_refs?: Record<string, string>;
  details?: string;
  resolution?: number;
  method?: string;
  space_group?: string;
  crystallography?: EntryCrystallography;
};

export type Entry = EntryProperties & {
  id: string;
  created_by: string;
  title: string | null;
  thumbnail_image_url: string | null;
  idempotency_key?: string | null;
  protein_sequences?: ProteinSequence[];
  polymer_entities?: PolymerEntity[];
  published_at?: string | null;
  created_at: string;
  updated_at: string;
};

export type Model = {
  id: string;
  entry_id: string;
  created_by: string;
  title: string | null;
  thumbnail_image_url: string | null;
  metadata?: JSONRecord;
  primary_artifact_id?: string | null;
  metrics?: Metric[];
  published_at?: string | null;
  created_at: string;
  updated_at: string;
};

export type EntityType = "data" | "metrics" | "model" | "program";
export type EntityLevel = "L0" | "L1" | "L2" | "L3";
export type ArtifactType =
  | "model"
  | "structure_factors"
  | "fasta"
  | "other";

export type FastaRecordMetadata = {
  header: string;
  sequence: string;
};

export type FastaMetadata = {
  records: FastaRecordMetadata[];
};

export type ProteinSequence = {
  id: string;
  source_artifact_id: string;
  record_index: number;
  header: string;
  sequence: string;
  created_at: string;
};

export type PolymerEntityOrganism = {
  scientific_name: string;
  ncbi_taxonomy_id?: number;
};

/** Where the accession came from: the wwPDB/EBI mapping, or the depositor's
 *  own struct_ref record. Only SIFTS mappings carry a UniProt release. */
export type PolymerEntityUniProtSource = "sifts" | "struct_ref";

export type PolymerEntityUniProtMapping = {
  accession: string;
  source: PolymerEntityUniProtSource;
  unp_release?: string;
};

export type ResidueData = {
  label_asym_id: string;
  label_seq_id: number;
  label_comp_id?: string;
  auth_asym_id?: string;
  auth_seq_id?: number;
  pdbx_pdb_ins_code?: string;
  label_alt_id?: string;
  uniprot_position?: string;
  rscc?: number;
  b_iso?: number;
  occupancy?: number;
  conformer_count?: number;
  rmsf?: number;
};

/** One stored polymer chain. Chains with the same sequence share a
 *  protein_sequence_id and can be grouped by the UI. */
export type PolymerEntity = {
  id: string;
  protein_sequence_id: string;
  label_entity_id?: string;
  label_asym_id?: string;
  auth_asym_id?: string;
  description?: string;
  source_organisms: PolymerEntityOrganism[];
  construct?: string;
  mutations?: string;
  uniprot_mappings: PolymerEntityUniProtMapping[];
  residue_data: ResidueData[];
  created_at: string;
};

type EntryDocument = {
  data: EntryData;
  included?: EntryProteinSequenceData[];
};

type EntryData = {
  type: "entries";
  id: string;
  attributes: EntryAttributes;
  relationships: EntryRelationships;
};

type EntryAttributes = EntryProperties & {
  title: string | null;
  thumbnail_image_url: string | null;
  polymer_entities?: PolymerEntity[];
  published_at?: string | null;
  created_at: string;
  updated_at: string;
};

type EntryRelationships = {
  created_by: {
    data: { type: "users"; id: string };
  };
  protein_sequences: {
    data: { type: "protein_sequences"; id: string }[];
  };
};

type EntryProteinSequenceData = {
  type: "protein_sequences";
  id: string;
  attributes: {
    record_index: number;
    header: string;
    sequence: string;
    created_at: string;
  };
  relationships: {
    source_artifact: {
      data: { type: "artifacts"; id: string };
    };
  };
};

export type SimilarEntryMatch = {
  source_sequence_id: string;
  similar_sequence: ProteinSequence;
  score: number;
  tool: string;
  /** Tool-specific alignment facts (fident, qcov, evalue, positions, aligned
   *  strings, ...). Free-form by design; lib/similarity.ts is the reader. */
  metadata: JSONRecord;
  created_at: string;
};

export type SimilarEntry = {
  entry: Entry;
  score: number;
  matches: SimilarEntryMatch[];
};

export type DataPayload = {
  file_url: string;
  type?: string;
  authors?: string[];
  size?: number;
  /** Lowercase hex digest of the stored file, as the artifact recorded it. */
  sha256?: string | null;
  metadata?: JSONRecord;
};

export type ModelPayload = {
  file_url: string;
  type?: string;
  authors?: string[];
  size?: number;
  sha256?: string | null;
  metadata?: JSONRecord;
};

export type MetricsPayload = {
  r_free?: number;
  r_work?: number;
  clashscore?: number;
  molprobity_score?: number;
  rscc?: number;
};

export type ProgramPayload = {
  name: string;
  version: string;
  description: string;
};

export type EntityPayload =
  | DataPayload
  | ModelPayload
  | MetricsPayload
  | ProgramPayload;

export type Entity = {
  id: string;
  entry_id: string;
  model_id: string | null;
  type: EntityType;
  level: EntityLevel | null;
  name: string;
  payload: EntityPayload;
  created_at: string;
  updated_at: string;
};

export type RelationType = "input_to" | "output_of" | "metrics_for";

export type EntityRelation = {
  id: string;
  source_entity_id: string;
  target_entity_id: string;
  relation_type: RelationType;
  created_at: string;
  updated_at: string;
};

type JSONAPIData<T> = {
  type: string;
  id?: string;
  attributes: T;
};

type JSONAPIDataDocument<T> = {
  data: JSONAPIData<T>;
};

type JSONAPIDataCollectionDocument<T> = {
  data: JSONAPIData<T>[];
  meta?: JSONRecord;
};

export type Artifact = {
  id: string;
  name: string;
  level: EntityLevel;
  type: ArtifactType;
  uri: string | null;
  sha256: string | null;
  format: string | null;
  size_bytes: number | null;
  metadata: JSONRecord;
  created_by: string;
  created_at: string;
};

export type Metric = {
  id: string;
  key: string;
  value: number;
  created_at: string;
};

type Run = {
  id: string;
  name: string;
  software_name: string | null;
  software_version: string | null;
  command: string | null;
  parameters: JSONRecord;
  metadata: JSONRecord;
  created_by: string;
  created_at: string;
  updated_at: string;
};

type RunArtifact = {
  run_id: string;
  artifact_id: string;
  direction: "input" | "output";
  position: number | null;
};

type ModelArtifactListResponse = {
  items: Artifact[];
  runs: Run[];
  relations: RunArtifact[];
};

type ModelArtifactCollectionDocument = {
  data: JSONAPIData<Artifact>[];
  meta: {
    runs: Run[];
    relations: RunArtifact[];
  };
};

type CreateArtifactRequest = {
  id: string;
  name: string;
  level: EntityLevel;
  type: ArtifactType;
  uri: string | null;
  sha256: string | null;
  format: string | null;
  size_bytes: number | null;
  metadata: JSONRecord;
};

type CreateMetricRequest = {
  id: string;
  key: string;
  value: number;
};

type CreateRunArtifactRequest = {
  artifact_id: string;
  direction: "input" | "output";
  position: number | null;
};

type CreateRunRequest = {
  id: string;
  name: string;
  software_name: string | null;
  software_version: string | null;
  command: string | null;
  parameters: JSONRecord;
  metadata: JSONRecord;
  started_at: string | null;
  finished_at: string | null;
  artifacts: CreateRunArtifactRequest[];
};

type BackendCreateModelData = {
  title: string;
  thumbnail_image_url?: string | null;
  idempotency_key?: string | null;
  metadata: JSONRecord;
  primary_artifact_id: string | null;
  artifacts: CreateArtifactRequest[];
  runs: CreateRunRequest[];
  metrics: CreateMetricRequest[];
};

type BackendCreateEntryRequest = {
  entry: EntryProperties & {
    title: string;
    thumbnail_image_url?: string | null;
    artifacts: CreateArtifactRequest[];
  };
  model_operations: {
    op: "add";
    data: BackendCreateModelData;
  }[];
};

export type CreateEntryRevisionResult = {
  entry_id: string;
  revision_id: string;
  base_revision_id?: string | null;
  state: "in_review";
  model_results: {
    op: "add";
    model_id: string;
    model_revision_id: string;
  }[];
};

export type CreateModelRevisionResult = {
  entry_id: string;
  model_id: string;
  revision_id: string;
  base_revision_id?: string | null;
  idempotency_key?: string | null;
  state: RevisionState;
};

export type EntryPageData = {
  entry: Entry;
  models: Model[];
  entities: Entity[];
  relations: EntityRelation[];
};

export type ModelPageData = {
  entry: Entry;
  model: Model;
  entities: Entity[];
  relations: EntityRelation[];
};

export class ApiRequestError extends Error {
  readonly status: number | null;

  constructor(message: string, status: number | null = null) {
    super(message);
    this.name = "ApiRequestError";
    this.status = status;
  }
}

export async function listEntries(
  token?: string,
  opts?: {
    query?: string | null;
    pdbIds?: string[] | null;
    limit?: number | null;
    offset?: number | null;
  },
): Promise<Entry[]> {
  const params = new URLSearchParams();
  const query = opts?.query?.trim();
  if (query) {
    params.set("query", query);
  }
  if (opts?.limit != null) {
    params.set("limit", String(opts.limit));
  }
  if (opts?.offset != null) {
    params.set("offset", String(opts.offset));
  }
  for (const pdbId of opts?.pdbIds ?? []) {
    const trimmed = pdbId.trim();
    if (trimmed) {
      params.append("pdb_id", trimmed);
    }
  }
  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const document = await fetchBackend<JSONAPIDataCollectionDocument<Entry>>(
    `/v1/entries${suffix}`,
    token,
  );
  return attributesFromCollection(document);
}

export async function listModels(
  token?: string,
  opts?: {
    limit?: number | null;
    offset?: number | null;
  },
): Promise<Model[]> {
  const params = new URLSearchParams();
  if (opts?.limit != null) {
    params.set("limit", String(opts.limit));
  }
  if (opts?.offset != null) {
    params.set("offset", String(opts.offset));
  }
  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const document = await fetchBackend<JSONAPIDataCollectionDocument<Model>>(
    `/v1/models${suffix}`,
    token,
  );
  return attributesFromCollection(document);
}

// Entries whose protein sequences a similarity run matched against this
// entry's, best overall score first. Scoring and ordering belong to the
// backend; the frontend only presents them.
export async function listSimilarEntries(
  token: string | undefined,
  entryId: string,
  opts?: { limit?: number | null; offset?: number | null },
): Promise<SimilarEntry[]> {
  const params = new URLSearchParams();
  if (opts?.limit != null) {
    params.set("limit", String(opts.limit));
  }
  if (opts?.offset != null) {
    params.set("offset", String(opts.offset));
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const document = await fetchBackend<JSONAPIDataCollectionDocument<SimilarEntry>>(
    `/v1/entries/${encodeURIComponent(entryId)}/similar-entries${suffix}`,
    token,
  );
  return attributesFromCollection(document);
}

export type CreateEntityInput = {
  id: string;
  type: EntityType;
  artifact_type?: ArtifactType;
  level?: EntityLevel | null;
  name: string;
  payload: JSONRecord;
};

export type CreateEntityRelationInput = {
  source_entity_id: string;
  target_entity_id: string;
  relation_type: string;
};

export type CreateModelInput = {
  title: string;
  thumbnail_image_url?: string | null;
  idempotency_key?: string | null;
  entities?: CreateEntityInput[];
  relations?: CreateEntityRelationInput[];
  /** Judgements a file cannot state about itself: what the run was for and
   *  what kind of model came out. Composition is read from the coordinates. */
  metadata?: JSONRecord;
};

export type CreateEntryInput = EntryProperties & {
  title: string;
  thumbnail_image_url?: string | null;
  entities?: CreateEntityInput[];
  models?: CreateModelInput[];
};

/** Creates a model and immediately places its initial revision in review. */
export async function createModel(
  token: string,
  entryId: string,
  input: CreateModelInput,
): Promise<CreateModelRevisionResult> {
  const document = await sendJSON<JSONAPIDataDocument<CreateModelRevisionResult>>(
    "POST",
    `/v1/entries/${encodeURIComponent(entryId)}/models`,
    token,
    { model: createModelData(input) },
  );
  return attributesFromDocument(document);
}

/** Creates an entry, its initial revision, and independent model revisions. */
export async function createEntry(
  token: string,
  input: CreateEntryInput,
): Promise<CreateEntryRevisionResult> {
  const document = await sendJSON<JSONAPIDataDocument<CreateEntryRevisionResult>>(
    "POST",
    "/v1/entries",
    token,
    createEntryRequest(input),
  );
  return attributesFromDocument(document);
}

// --- Review workflow -------------------------------------------------------

export type RevisionState =
  | "pending"
  | "in_review"
  | "active"
  | "rejected"
  | "archived";

export type EntryRevisionSummary = {
  id: string;
  entry_id: string;
  parent_revision_id?: string | null;
  revision_number?: number | null;
  state: RevisionState;
  entry_state: "new" | "active" | "deleted";
  created_by: string;
  title: string | null;
  published_at?: string | null;
  created_at: string;
  updated_at: string;
};

export type ModelRevisionSummary = {
  id: string;
  entry_id: string;
  model_id: string;
  idempotency_key?: string | null;
  parent_revision_id?: string | null;
  revision_number?: number | null;
  state: RevisionState;
  model_state: "new" | "active" | "deleted";
  created_by: string;
  title: string | null;
  published_at?: string | null;
  created_at: string;
  updated_at: string;
};

export type EntryRevisionGroup = {
  entry: Entry;
  entry_revisions: EntryRevisionSummary[];
  model_revisions: ModelRevisionSummary[];
};

export type RevisionTarget =
  | {
      kind: "entry";
      entry_id: string;
      revision_id: string;
    }
  | {
      kind: "model";
      entry_id: string;
      model_id: string;
      revision_id: string;
    };

/** One row of the queue: always an entry. Everything waiting under it — its own
 *  revision and each of its model revisions — is decided from that row's card,
 *  each on its own. The summaries ride along so opening a row costs only the
 *  revisions it actually shows, not a second pass over the whole queue. */
export type ReviewQueueItem = {
  entry_id: string;
  title: string;
  submitted_at: string;
  entry_revisions: EntryRevisionSummary[];
  model_revisions: ModelRevisionSummary[];
};

export type ReviewQueuePage = {
  items: ReviewQueueItem[];
  /** A full page came back, so there is probably another. The endpoint returns
   *  no total, so this is the only signal there is. */
  hasMore: boolean;
};



export type EntryRevision = EntryRevisionSummary & EntryProperties & {
  thumbnail_image_url: string | null;
  protein_sequences: ProteinSequence[];
  artifacts: Artifact[];
};

export type ModelRevision = ModelRevisionSummary & {
  thumbnail_image_url: string | null;
  primary_artifact_id: string | null;
  metadata: JSONRecord;
  metrics: Metric[];
  artifacts: Artifact[];
};

export type ReviewEntry = EntryRevision;
export type ReviewModel = ModelRevision;

/** One reviewable thing: the revision waiting for a decision, the published one
 *  it replaces (null when there is none), and the address to decide it at. */
export type ReviewPair<T> = {
  target: RevisionTarget;
  active: T | null;
  proposed: T;
};

export type EntryReview = {
  entry_id: string;
  title: string;
  entry: ReviewPair<ReviewEntry> | null;
  models: (ReviewPair<ReviewModel> & { model_id: string })[];
};

export type RevisionDecision = "active" | "rejected";

export async function submitModelRevision(
  token: string,
  userId: string,
  entryId: string,
  modelId: string,
  revisionId: string,
): Promise<void> {
  await sendJSON(
    "PATCH",
    `/v1/users/${encodeURIComponent(userId)}/entries/${encodeURIComponent(
      entryId,
    )}/models/${encodeURIComponent(modelId)}/revisions/${encodeURIComponent(
      revisionId,
    )}`,
    token,
    { state: "in_review" },
  );
}

export async function listReviews(
  token: string,
  opts?: { limit?: number; offset?: number },
): Promise<ReviewQueuePage> {
  const limit = opts?.limit ?? REVIEW_PAGE_SIZE;
  const offset = opts?.offset ?? 0;
  const groups = await listInReviewGroups(token, limit, offset);
  // The backend orders groups by their newest revision first and paginates in
  // that order, so the rows are left exactly as they arrive: re-sorting here
  // would tear page boundaries apart.
  const items = groups.map(queueItemFromGroup);
  return { items, hasMore: items.length === limit };
}

export async function getEntryReview(
  token: string,
  item: ReviewQueueItem,
): Promise<EntryReview> {
  return buildEntryReview(reviewerReaders(token), item);
}

/** Finds one entry's row without knowing which page it is on. Only the preview
 *  pages need this: they are opened by URL, with no queue in hand. */
export async function findReviewItem(
  token: string,
  entryId: string,
): Promise<ReviewQueueItem | null> {
  for (let offset = 0; ; offset += REVIEW_PAGE_SIZE) {
    const page = await listReviews(token, {
      limit: REVIEW_PAGE_SIZE,
      offset,
    });
    const found = page.items.find((item) => item.entry_id === entryId);
    if (found) {
      return found;
    }
    if (!page.hasMore) {
      return null;
    }
  }
}

type RevisionReaders = {
  entry: (entryId: string, revisionId: string) => Promise<EntryRevision>;
  model: (
    entryId: string,
    modelId: string,
    revisionId: string,
  ) => Promise<ModelRevision>;
};

function reviewerReaders(token: string): RevisionReaders {
  return {
    entry: (entryId, revisionId) => getEntryRevision(token, entryId, revisionId),
    model: (entryId, modelId, revisionId) =>
      getModelRevision(token, entryId, modelId, revisionId),
  };
}

/** An author reads their own revisions through the user-scoped routes; the
 *  reviewer ones are refused to anyone without review access. */
function authorReaders(token: string, userId: string): RevisionReaders {
  return {
    entry: async (entryId, revisionId) =>
      attributesFromDocument(
        await fetchBackend<JSONAPIDataDocument<EntryRevision>>(
        `/v1/users/${encodeURIComponent(userId)}/entries/${encodeURIComponent(
          entryId,
        )}/revisions/${encodeURIComponent(revisionId)}`,
        token,
      ),
      ),
    model: async (entryId, modelId, revisionId) =>
      attributesFromDocument(
        await fetchBackend<JSONAPIDataDocument<ModelRevision>>(
        `/v1/users/${encodeURIComponent(userId)}/entries/${encodeURIComponent(
          entryId,
        )}/models/${encodeURIComponent(modelId)}/revisions/${encodeURIComponent(
          revisionId,
        )}`,
        token,
      ),
      ),
  };
}

/** An author's own revision, read through the user-scoped route. The reviewer
 *  routes refuse everyone without review access, so a preview opened by the
 *  person who submitted it has to come through here. */
export async function getUserEntryRevision(
  token: string,
  userId: string,
  entryId: string,
  revisionId: string,
): Promise<EntryRevision> {
  return authorReaders(token, userId).entry(entryId, revisionId);
}

export async function getUserModelRevision(
  token: string,
  userId: string,
  entryId: string,
  modelId: string,
  revisionId: string,
): Promise<ModelRevision> {
  return authorReaders(token, userId).model(entryId, modelId, revisionId);
}

async function buildEntryReview(
  readers: RevisionReaders,
  item: ReviewQueueItem,
): Promise<EntryReview> {
  const [entry, models] = await Promise.all([
    item.entry_revisions.length > 0
      ? entryPair(readers, item.entry_id, item.entry_revisions[0])
      : null,
    Promise.all(
      item.model_revisions.map((revision) => modelPair(readers, revision)),
    ),
  ]);
  return { entry_id: item.entry_id, title: item.title, entry, models };
}

async function entryPair(
  readers: RevisionReaders,
  entryId: string,
  summary: EntryRevisionSummary,
): Promise<ReviewPair<EntryRevision>> {
  const proposed = await readers.entry(entryId, summary.id);
  const active = proposed.parent_revision_id
    ? await readers.entry(entryId, proposed.parent_revision_id)
    : null;
  return {
    target: { kind: "entry", entry_id: entryId, revision_id: summary.id },
    active,
    proposed,
  };
}

async function modelPair(
  readers: RevisionReaders,
  summary: ModelRevisionSummary,
): Promise<ReviewPair<ModelRevision> & { model_id: string }> {
  const proposed = await readers.model(
    summary.entry_id,
    summary.model_id,
    summary.id,
  );
  const active = proposed.parent_revision_id
    ? await readers.model(
        summary.entry_id,
        summary.model_id,
        proposed.parent_revision_id,
      )
    : null;
  return {
    model_id: summary.model_id,
    target: {
      kind: "model",
      entry_id: summary.entry_id,
      model_id: summary.model_id,
      revision_id: summary.id,
    },
    active,
    proposed,
  };
}

/** The author's own submissions in one state, grouped by entry exactly like the
 *  admin queue so both screens can render from the same shape. */
export async function listUserSubmissions(
  token: string,
  userId: string,
  state: RevisionState,
  opts?: { limit?: number; offset?: number },
): Promise<ReviewQueuePage> {
  const limit = opts?.limit ?? REVIEW_PAGE_SIZE;
  const offset = opts?.offset ?? 0;
  const params = new URLSearchParams({
    state,
    limit: String(limit),
    offset: String(offset),
  });
  const document = await fetchBackend<JSONAPIDataCollectionDocument<EntryRevisionGroup>>(
    `/v1/users/${encodeURIComponent(userId)}/entries/revisions?${params.toString()}`,
    token,
  );
  const items = attributesFromCollection(document).map(queueItemFromGroup);
  return { items, hasMore: items.length === limit };
}

export async function getUserSubmission(
  token: string,
  userId: string,
  item: ReviewQueueItem,
): Promise<EntryReview> {
  return buildEntryReview(authorReaders(token, userId), item);
}

export async function findUserSubmissionItem(
  token: string,
  userId: string,
  state: RevisionState,
  entryId: string,
): Promise<ReviewQueueItem | null> {
  for (let offset = 0; ; offset += REVIEW_PAGE_SIZE) {
    const page = await listUserSubmissions(token, userId, state, {
      limit: REVIEW_PAGE_SIZE,
      offset,
    });
    const found = page.items.find((candidate) => candidate.entry_id === entryId);
    if (found) {
      return found;
    }
    if (!page.hasMore) {
      return null;
    }
  }
}

async function listInReviewGroups(
  token: string,
  limit: number,
  offset: number,
): Promise<EntryRevisionGroup[]> {
  const params = new URLSearchParams({
    state: "in_review",
    limit: String(limit),
    offset: String(offset),
  });
  const document = await fetchBackend<JSONAPIDataCollectionDocument<EntryRevisionGroup>>(
    `/v1/entries/revisions?${params.toString()}`,
    token,
  );
  return attributesFromCollection(document);
}

function queueItemFromGroup(group: EntryRevisionGroup): ReviewQueueItem {
  return {
    entry_id: group.entry.id,
    title: formatEntryLabel(group.entry),
    submitted_at: earliest([
      ...group.entry_revisions.map((revision) => revision.updated_at),
      ...group.model_revisions.map((revision) => revision.updated_at),
    ]),
    entry_revisions: group.entry_revisions,
    model_revisions: group.model_revisions,
  };
}

function earliest(values: string[]): string {
  return values.reduce(
    (soonest, value) => (value < soonest ? value : soonest),
    values[0] ?? "",
  );
}

export async function decideEntryReview(
  token: string,
  target: RevisionTarget,
  state: RevisionDecision,
): Promise<void> {
  const path =
    target.kind === "entry"
      ? `/v1/entries/${encodeURIComponent(
          target.entry_id,
        )}/revisions/${encodeURIComponent(target.revision_id)}`
      : `/v1/entries/${encodeURIComponent(
          target.entry_id,
        )}/models/${encodeURIComponent(
          target.model_id,
        )}/revisions/${encodeURIComponent(target.revision_id)}`;
  await sendJSON(
    "PATCH",
    path,
    token,
    { state },
  );
}

export async function listUserEntryRevisionGroups(
  token: string,
  userId: string,
  state: RevisionState,
): Promise<EntryRevisionGroup[]> {
  const document = await fetchBackend<JSONAPIDataCollectionDocument<EntryRevisionGroup>>(
    `/v1/users/${encodeURIComponent(
      userId,
    )}/entries/revisions?state=${encodeURIComponent(state)}`,
    token,
  );
  return attributesFromCollection(document);
}

export async function getEntryRevision(
  token: string,
  entryId: string,
  revisionId: string,
): Promise<EntryRevision> {
  const document = await fetchBackend<JSONAPIDataDocument<EntryRevision>>(
    `/v1/entries/${encodeURIComponent(entryId)}/revisions/${encodeURIComponent(revisionId)}`,
    token,
  );
  return attributesFromDocument(document);
}

export async function getModelRevision(
  token: string,
  entryId: string,
  modelId: string,
  revisionId: string,
): Promise<ModelRevision> {
  const document = await fetchBackend<JSONAPIDataDocument<ModelRevision>>(
    `/v1/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(
      modelId,
    )}/revisions/${encodeURIComponent(revisionId)}`,
    token,
  );
  return attributesFromDocument(document);
}


async function sendJSON<T>(
  method: string,
  path: string,
  token: string,
  input: unknown,
): Promise<T> {
  const baseUrl = getApiBaseUrl().replace(/\/+$/, "");
  let response: Response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
      method,
      cache: "no-store",
      headers: {
        Accept: jsonApiMediaType,
        "Content-Type": jsonApiMediaType,
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(input),
    });
  } catch (error) {
    throw new ApiRequestError(
      error instanceof Error
        ? `Backend request failed: ${error.message}`
        : "Backend request failed",
    );
  }
  if (!response.ok) {
    throw new ApiRequestError(
      `Backend responded with ${response.status}`,
      response.status,
    );
  }
  return (await response.json()) as T;
}

/** Fetches a single entry without its models or artifacts. Cheap enough to
 *  call per row when a list only needs entry titles. */
export async function getEntry(
  token: string | undefined,
  entryId: string,
): Promise<Entry> {
  const document = await fetchBackend<EntryDocument>(
    `/v1/entries/${encodeURIComponent(entryId)}`,
    token,
  );
  return entryFromDocument(document);
}

export async function getEntryPageData(
  token: string | undefined,
  entryId: string,
): Promise<EntryPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const [entry, modelsDocument, entryArtifactsDocument] = await Promise.all([
    getEntry(token, entryId),
    fetchBackend<JSONAPIDataCollectionDocument<Model>>(
      `/v1/entries/${encodedEntryId}/models`,
      token,
    ),
    fetchBackend<JSONAPIDataCollectionDocument<Artifact>>(
      `/v1/entries/${encodedEntryId}/artifacts`,
      token,
    ),
  ]);
  const models = attributesFromCollection(modelsDocument);
  const entryArtifacts = attributesFromCollection(entryArtifactsDocument);
  const modelGraphs = await Promise.all(
    models.map((model) => fetchModelGraph(token, entry.id, model)),
  );

  return {
    entry,
    models,
    entities: [
      ...entryArtifacts.map((artifact) =>
        entityFromArtifact(entry.id, null, artifact),
      ),
      ...modelGraphs.flatMap((graph) => graph.entities),
    ],
    relations: modelGraphs.flatMap((graph) => graph.relations),
  };
}

export async function getModelPageData(
  token: string | undefined,
  entryId: string,
  modelId: string,
): Promise<ModelPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const encodedModelId = encodeURIComponent(modelId);
  const [entry, modelDocument] = await Promise.all([
    getEntry(token, entryId),
    fetchBackend<JSONAPIDataDocument<Model>>(
      `/v1/entries/${encodedEntryId}/models/${encodedModelId}`,
      token,
    ),
  ]);
  const model = attributesFromDocument(modelDocument);
  const graph = await fetchModelGraph(token, entry.id, model);

  return {
    entry,
    model,
    entities: graph.entities,
    relations: graph.relations,
  };
}

export type EntryGraph = {
  entities: Entity[];
  relations: EntityRelation[];
};

export async function getEntryGraph(
  token: string | undefined,
  entryId: string,
): Promise<EntryGraph> {
  const data = await getEntryPageData(token, entryId);
  return { entities: data.entities, relations: data.relations };
}

function attributesFromDocument<T>(document: JSONAPIDataDocument<T>): T {
  return document.data.attributes;
}

function attributesFromCollection<T>(
  document: JSONAPIDataCollectionDocument<T>,
): T[] {
  return document.data.map((item) => item.attributes);
}

function modelArtifactListFromDocument(
  document: ModelArtifactCollectionDocument,
): ModelArtifactListResponse {
  return {
    items: attributesFromCollection(document),
    runs: document.meta.runs,
    relations: document.meta.relations,
  };
}

function entryFromDocument(document: EntryDocument): Entry {
  const data = document.data;
  const attributes = data.attributes;
  const sequencesById = new Map(
    (document.included ?? [])
      .filter((included) => included.type === "protein_sequences")
      .map((included) => [included.id, included]),
  );
  const proteinSequences = data.relationships.protein_sequences.data
    .map((identifier) => sequencesById.get(identifier.id))
    .filter((sequence): sequence is EntryProteinSequenceData => sequence != null)
    .map((sequence) => ({
      id: sequence.id,
      source_artifact_id: sequence.relationships.source_artifact.data.id,
      record_index: sequence.attributes.record_index,
      header: sequence.attributes.header,
      sequence: sequence.attributes.sequence,
      created_at: sequence.attributes.created_at,
    }));

  return {
    id: data.id,
    created_by: data.relationships.created_by.data.id,
    title: attributes.title,
    thumbnail_image_url: attributes.thumbnail_image_url,
    ...definedEntryProperties(attributes),
    protein_sequences: proteinSequences,
    polymer_entities: attributes.polymer_entities ?? [],
    published_at: attributes.published_at,
    created_at: attributes.created_at,
    updated_at: attributes.updated_at,
  };
}

function definedEntryProperties(properties: EntryProperties): EntryProperties {
  return {
    ...(properties.external_refs !== undefined
      ? { external_refs: properties.external_refs }
      : {}),
    ...(properties.details !== undefined ? { details: properties.details } : {}),
    ...(properties.resolution !== undefined
      ? { resolution: properties.resolution }
      : {}),
    ...(properties.method !== undefined ? { method: properties.method } : {}),
    ...(properties.space_group !== undefined
      ? { space_group: properties.space_group }
      : {}),
    ...(properties.crystallography !== undefined
      ? { crystallography: properties.crystallography }
      : {}),
  };
}

async function fetchModelGraph(
  token: string | undefined,
  entryId: string,
  model: Model,
): Promise<EntryGraph> {
  const document = await fetchBackend<ModelArtifactCollectionDocument>(
    `/v1/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(
      model.id,
    )}/artifacts`,
    token,
  );
  return modelGraphFromBackend(entryId, model, modelArtifactListFromDocument(document));
}

function createEntryRequest(input: CreateEntryInput): BackendCreateEntryRequest {
  const entities = input.entities ?? [];
  return {
    entry: {
      title: input.title,
      thumbnail_image_url: input.thumbnail_image_url,
      external_refs: input.external_refs,
      details: input.details,
      resolution: input.resolution,
      method: input.method,
      space_group: input.space_group,
      crystallography: input.crystallography,
      artifacts: entities.filter(isArtifactEntity).map(createArtifactRequest),
    },
    model_operations: (input.models ?? []).map((model) => ({
      op: "add" as const,
      data: createModelData(model),
    })),
  };
}

function createModelData(input: CreateModelInput): BackendCreateModelData {
  const entities = input.entities ?? [];
  const relations = input.relations ?? [];
  const artifacts = entities.filter(isArtifactEntity).map(createArtifactRequest);
  const primaryModelEntity =
    entities.find((entity) => entity.type === "model") ?? null;

  return {
    title: input.title,
    thumbnail_image_url: input.thumbnail_image_url,
    idempotency_key:
      stringOrNull(input.idempotency_key) ??
      modelRevisionIdempotencyKey(artifacts, primaryModelEntity?.id ?? null),
    metadata: {
      ...modelMetadataFromEntity(primaryModelEntity),
      ...(input.metadata ?? {}),
    },
    primary_artifact_id: primaryModelEntity?.id ?? null,
    artifacts,
    runs: entities
      .filter((entity) => entity.type === "program")
      .map((entity) => createRunRequest(entity, relations)),
    metrics: entities
      .filter((entity) => entity.type === "metrics")
      .flatMap(createMetricRequests),
  };
}

function createArtifactRequest(entity: CreateEntityInput): CreateArtifactRequest {
  const payload = objectRecord(entity.payload);
  const metadata = objectRecord(payload.metadata);
  return {
    id: entity.id,
    name: entity.name,
    level: entity.level ?? "L0",
    type: artifactTypeFromEntity(entity),
    uri: stringOrNull(payload.file_url),
    sha256: stringOrNull(payload.sha256),
    format: artifactFormat(entity),
    size_bytes: numberOrNull(payload.size),
    metadata,
  };
}

function modelRevisionIdempotencyKey(
  artifacts: CreateArtifactRequest[],
  primaryArtifactId: string | null,
): string | null {
  const primary = artifacts.find((artifact) => artifact.id === primaryArtifactId);
  return (
    idempotencyKeyFromSHA256(primary?.sha256) ??
    idempotencyKeyFromSHA256(
      artifacts.find((artifact) => stringOrNull(artifact.sha256))?.sha256,
    )
  );
}

function idempotencyKeyFromSHA256(sha256: string | null | undefined): string | null {
  const trimmed = stringOrNull(sha256)?.toLowerCase();
  return trimmed ?? null;
}

function createRunRequest(
  entity: CreateEntityInput,
  relations: CreateEntityRelationInput[],
): CreateRunRequest {
  const payload = objectRecord(entity.payload);
  const name = stringOrNull(payload.name) ?? entity.name;
  const description = stringOrNull(payload.description);
  const metadata: JSONRecord = {};
  if (description) {
    metadata.description = description;
  }
  const artifacts: CreateRunArtifactRequest[] = [];
  for (const relation of relations) {
    if (relation.target_entity_id !== entity.id) {
      continue;
    }
    if (relation.relation_type === "input_to") {
      artifacts.push({
        artifact_id: relation.source_entity_id,
        direction: "input",
        position: null,
      });
    }
    if (relation.relation_type === "output_of") {
      artifacts.push({
        artifact_id: relation.source_entity_id,
        direction: "output",
        position: null,
      });
    }
  }

  return {
    id: entity.id,
    name,
    software_name: name,
    software_version: stringOrNull(payload.version),
    command: null,
    parameters: {},
    metadata,
    started_at: null,
    finished_at: null,
    artifacts,
  };
}

function createMetricRequests(entity: CreateEntityInput): CreateMetricRequest[] {
  return Object.entries(entity.payload).flatMap(([key, value]) =>
    typeof value === "number" && Number.isFinite(value)
      ? [{ id: stableUUID(`${entity.id}:${key}`), key, value }]
      : [],
  );
}

/** A model revision as the same entity graph the published model page walks, so
 *  a preview gets the real Data levels and Evaluations tiles instead of a
 *  hand-rolled stand-in. A revision carries no runs, so program nodes and their
 *  edges are absent — nothing that needs levels or metrics depends on them. */
export function modelRevisionGraph(revision: ModelRevision): EntryGraph {
  const model = modelFromRevision(revision);
  const entities = revision.artifacts.map((artifact) =>
    entityFromArtifact(revision.entry_id, revision.model_id, artifact, model),
  );
  const relations: EntityRelation[] = [];

  const metricsEntity = entityFromMetrics(revision.entry_id, model);
  if (metricsEntity) {
    entities.push(metricsEntity);
    if (model.primary_artifact_id) {
      relations.push({
        id: stableUUID(
          `${metricsEntity.id}:${model.primary_artifact_id}:metrics_for`,
        ),
        source_entity_id: metricsEntity.id,
        target_entity_id: model.primary_artifact_id,
        relation_type: "metrics_for",
        created_at: model.created_at,
        updated_at: model.updated_at,
      });
    }
  }

  return { entities, relations };
}

/** The published-model shape of a revision: same content, addressed by model id
 *  rather than revision id, which is what the view helpers expect. */
export function modelFromRevision(revision: ModelRevision): Model {
  return {
    id: revision.model_id,
    entry_id: revision.entry_id,
    created_by: revision.created_by,
    title: revision.title,
    thumbnail_image_url: revision.thumbnail_image_url,
    metadata: revision.metadata,
    primary_artifact_id: revision.primary_artifact_id,
    metrics: revision.metrics,
    published_at: revision.published_at,
    created_at: revision.created_at,
    updated_at: revision.updated_at,
  };
}

function modelGraphFromBackend(
  entryId: string,
  model: Model,
  response: ModelArtifactListResponse,
): EntryGraph {
  const entities = [
    ...response.items.map((artifact) =>
      entityFromArtifact(entryId, model.id, artifact, model),
    ),
    ...response.runs.map((run) => entityFromRun(entryId, model.id, run)),
  ];
  const relations = response.relations.map((relation) =>
    entityRelationFromRunArtifact(relation, model),
  );

  const metricsEntity = entityFromMetrics(entryId, model);
  if (metricsEntity) {
    entities.push(metricsEntity);
    if (model.primary_artifact_id) {
      relations.push({
        id: stableUUID(
          `${metricsEntity.id}:${model.primary_artifact_id}:metrics_for`,
        ),
        source_entity_id: metricsEntity.id,
        target_entity_id: model.primary_artifact_id,
        relation_type: "metrics_for",
        created_at: model.created_at,
        updated_at: model.updated_at,
      });
    }
  }

  return { entities, relations };
}

function entityFromArtifact(
  entryId: string,
  modelId: string | null,
  artifact: Artifact,
  model?: Model,
): Entity {
  const entityType = artifactEntityType(artifact, model);
  // Level, format, size and checksum are all facts of the artifact, and the
  // file table prints each of them in its own column. Format used to be copied
  // for data artifacts only, so a model arrived without one, and the checksum
  // was dropped here entirely — stored by the backend, never shown.
  const payload: JSONRecord = {
    ...(artifact.uri ? { file_url: artifact.uri } : {}),
    ...(artifact.size_bytes != null ? { size: artifact.size_bytes } : {}),
    ...(artifact.format ? { type: artifact.format } : {}),
    ...(artifact.sha256 ? { sha256: artifact.sha256 } : {}),
    ...(Object.keys(artifact.metadata ?? {}).length > 0
      ? { metadata: artifact.metadata }
      : {}),
  };
  if (entityType === "model") {
    const metadata = objectRecord(model?.metadata);
    const authors = stringArray(metadata.authors);
    if (authors.length > 0) {
      payload.authors = authors;
    }
  }

  return {
    id: artifact.id,
    entry_id: entryId,
    model_id: modelId,
    type: entityType,
    level: artifact.level,
    name: artifact.name,
    payload: payload as EntityPayload,
    created_at: artifact.created_at,
    updated_at: artifact.created_at,
  };
}

function entityFromRun(entryId: string, modelId: string, run: Run): Entity {
  const description = stringOrNull(run.metadata?.description) ?? "";
  return {
    id: run.id,
    entry_id: entryId,
    model_id: modelId,
    type: "program",
    level: null,
    name: run.name,
    payload: {
      name: run.software_name ?? run.name,
      version: run.software_version ?? "",
      description,
    },
    created_at: run.created_at,
    updated_at: run.updated_at,
  };
}

function entityFromMetrics(entryId: string, model: Model): Entity | null {
  const metrics = model.metrics ?? [];
  if (metrics.length === 0) {
    return null;
  }
  const payload: MetricsPayload = {};
  for (const metric of metrics) {
    if (typeof metric.value === "number" && Number.isFinite(metric.value)) {
      payload[metric.key as keyof MetricsPayload] = metric.value;
    }
  }
  if (Object.keys(payload).length === 0) {
    return null;
  }
  return {
    id: stableUUID(`${model.id}:metrics`),
    entry_id: entryId,
    model_id: model.id,
    type: "metrics",
    level: "L3",
    name: "Metrics",
    payload,
    created_at: metrics[0]?.created_at ?? model.created_at,
    updated_at: metrics.at(-1)?.created_at ?? model.updated_at,
  };
}

function entityRelationFromRunArtifact(
  link: RunArtifact,
  model: Model,
): EntityRelation {
  const relationType = link.direction === "output" ? "output_of" : "input_to";
  return {
    id: stableUUID(`${link.artifact_id}:${link.run_id}:${relationType}`),
    source_entity_id: link.artifact_id,
    target_entity_id: link.run_id,
    relation_type: relationType,
    created_at: model.created_at,
    updated_at: model.updated_at,
  };
}

function artifactEntityType(artifact: Artifact, model?: Model): EntityType {
  if (model?.primary_artifact_id === artifact.id || artifact.type === "model") {
    return "model";
  }
  return "data";
}

function isArtifactEntity(entity: CreateEntityInput): boolean {
  return entity.type === "data" || entity.type === "model";
}

function artifactFormat(entity: CreateEntityInput): string | null {
  const payload = objectRecord(entity.payload);
  const explicit = stringOrNull(payload.type);
  if (explicit) {
    return explicit;
  }
  return formatFromNameOrURL(entity.name, stringOrNull(payload.file_url));
}

function artifactTypeFromEntity(entity: CreateEntityInput): ArtifactType {
  if (entity.artifact_type) {
    return entity.artifact_type;
  }
  if (entity.type === "model") {
    return "model";
  }
  return artifactFormat(entity) === "fasta" ? "fasta" : "other";
}

function modelMetadataFromEntity(entity: CreateEntityInput | null): JSONRecord {
  if (!entity) {
    return {};
  }
  const payload = objectRecord(entity.payload);
  const metadata: JSONRecord = {};
  const authors = stringArray(payload.authors);
  if (authors.length > 0) {
    metadata.authors = authors;
  }

  // Composition was read out of the coordinates and parked on the artifact.
  // The pages show it as a property of the model, so it is copied across
  // rather than left where only a file preview would find it.
  const parsed = objectRecord(payload.metadata);
  const details = stringOrNull(parsed.details);
  if (details) {
    metadata.details = details;
  }
  for (const key of [
    "atom_count",
    "modeled_residues",
    "unique_protein_chains",
    "altloc_fraction",
    "unmodeled_fraction",
  ] as const) {
    const value = numberOrNull(parsed[key]);
    if (value !== null) {
      metadata[key] = value;
    }
  }
  const ligands = stringArray(parsed.ligands);
  if (ligands.length > 0) {
    metadata.ligands = ligands;
  }
  const cofactors = stringArray(parsed.cofactors);
  if (cofactors.length > 0) {
    metadata.cofactors = cofactors;
  }

  return metadata;
}

function objectRecord(value: unknown): JSONRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as JSONRecord)
    : {};
}

function stringOrNull(value: unknown): string | null {
  return typeof value === "string" && value.trim() !== ""
    ? value.trim()
    : null;
}

function numberOrNull(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value
        .filter((item): item is string => typeof item === "string")
        .map((item) => item.trim())
        .filter(Boolean)
    : [];
}

function formatFromNameOrURL(name: string, url: string | null): string | null {
  const value = (url ?? name).split("?")[0]?.toLowerCase() ?? "";
  if (value.endsWith(".pdb")) {
    return "pdb";
  }
  if (value.endsWith(".cif") || value.endsWith(".mmcif")) {
    return "mmcif";
  }
  if (value.endsWith(".mtz")) {
    return "mtz";
  }
  if (value.endsWith(".ccp4") || value.endsWith(".map")) {
    return "ccp4";
  }
  if (
    value.endsWith(".fasta") ||
    value.endsWith(".fa") ||
    value.endsWith(".faa")
  ) {
    return "fasta";
  }
  return null;
}

function stableUUID(value: string): string {
  const bytes = Buffer.from(createHash("sha1").update(value).digest("hex"), "hex");
  bytes[6] = (bytes[6] & 0x0f) | 0x50;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = bytes.subarray(0, 16).toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(
    12,
    16,
  )}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

async function fetchBackend<T>(path: string, token?: string): Promise<T> {
  const baseUrl = getApiBaseUrl().replace(/\/+$/, "");

  const headers: Record<string, string> = { Accept: jsonApiMediaType };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  let response: Response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
      cache: "no-store",
      headers,
    });
  } catch (error) {
    throw new ApiRequestError(
      error instanceof Error
        ? `Backend request failed: ${error.message}`
        : "Backend request failed",
    );
  }

  if (!response.ok) {
    throw new ApiRequestError(
      `Backend responded with ${response.status}`,
      response.status,
    );
  }

  return (await response.json()) as T;
}
