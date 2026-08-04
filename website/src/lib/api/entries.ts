import "server-only";

import { createHash } from "node:crypto";

import { getApiBaseUrl } from "./baseUrl";

type JSONRecord = Record<string, unknown>;

export type Entry = {
  id: string;
  created_by: string;
  name: string;
  description: string | null;
  thumbnail_image_url: string | null;
  metadata?: JSONRecord;
  protein_sequences?: ProteinSequence[];
  published_at?: string | null;
  created_at: string;
  updated_at: string;
};

export type Model = {
  id: string;
  entry_id: string;
  created_by: string;
  name: string;
  description: string | null;
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

export type DataPayload = {
  file_url: string;
  type?: string;
  authors?: string[];
  affiliation?: string | null;
  size?: number;
  metadata?: JSONRecord;
};

export type ModelPayload = {
  file_url: string;
  authors?: string[];
  affiliation?: string | null;
  size?: number;
  metadata?: JSONRecord;
};

export type MetricsPayload = {
  r_free?: number;
  r_work?: number;
  rscc?: number;
  cc?: number;
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

type ListResponse<T> = {
  items: T[];
};

type Artifact = {
  id: string;
  name: string;
  level: EntityLevel;
  uri: string | null;
  sha256: string | null;
  format: string | null;
  size_bytes: number | null;
  metadata: JSONRecord;
  created_by: string;
  created_at: string;
};

type Metric = {
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

type CreateArtifactRequest = {
  id: string;
  name: string;
  level: EntityLevel;
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

type BackendCreateModelRequest = {
  id?: string;
  name: string;
  description?: string | null;
  thumbnail_image_url?: string | null;
  metadata: JSONRecord;
  primary_artifact_id: string | null;
  artifacts: CreateArtifactRequest[];
  runs: CreateRunRequest[];
  metrics: CreateMetricRequest[];
};

type BackendCreateEntryRequest = {
  id?: string;
  name: string;
  description?: string | null;
  thumbnail_image_url?: string | null;
  metadata: JSONRecord;
  artifacts: CreateArtifactRequest[];
  models: BackendCreateModelRequest[];
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
  opts?: { query?: string | null },
): Promise<Entry[]> {
  const params = new URLSearchParams();
  const query = opts?.query?.trim();
  if (query) {
    params.set("query", query);
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const response = await fetchBackend<ListResponse<Entry>>(
    `/v1/entries${suffix}`,
    token,
  );
  return response.items;
}

export type CreateEntityInput = {
  id: string;
  type: EntityType;
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
  id?: string;
  name: string;
  description?: string | null;
  thumbnail_image_url?: string | null;
  entities?: CreateEntityInput[];
  relations?: CreateEntityRelationInput[];
  /** Judgements a file cannot state about itself: what the run was for and
   *  what kind of model came out. Composition is read from the coordinates. */
  metadata?: JSONRecord;
};

export type CreateEntryMetadata = {
  pdb?: string | null;
  resolution?: number | null;
  organism?: string | null;
  method?: string | null;
  space_group?: string | null;
};

export type CreateEntryInput = {
  id?: string;
  name: string;
  description?: string | null;
  thumbnail_image_url?: string | null;
  entities?: CreateEntityInput[];
  models?: CreateModelInput[];
  /** Properties of the structure rather than of any one model. */
  metadata?: CreateEntryMetadata;
};

export async function createModel(
  token: string,
  entryId: string,
  input: CreateModelInput,
): Promise<void> {
  await postJSON(
    `/v1/entries/${encodeURIComponent(entryId)}/models`,
    token,
    createModelRequest(input),
  );
}

export async function createEntry(
  token: string,
  input: CreateEntryInput,
): Promise<void> {
  await postJSON("/v1/entries", token, createEntryRequest(input));
}

async function postJSON(
  path: string,
  token: string,
  input: unknown,
): Promise<void> {
  const baseUrl = getApiBaseUrl().replace(/\/+$/, "");
  let response: Response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
      method: "POST",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
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
}

export async function getEntryPageData(
  token: string | undefined,
  entryId: string,
): Promise<EntryPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const [entry, models, entryArtifacts] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<ListResponse<Model>>(
      `/v1/entries/${encodedEntryId}/models`,
      token,
    ),
    fetchBackend<ListResponse<Artifact>>(
      `/v1/entries/${encodedEntryId}/artifacts`,
      token,
    ),
  ]);
  const modelGraphs = await Promise.all(
    models.items.map((model) => fetchModelGraph(token, entry.id, model)),
  );

  return {
    entry,
    models: models.items,
    entities: [
      ...entryArtifacts.items.map((artifact) =>
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
  const [entry, model] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<Model>(
      `/v1/entries/${encodedEntryId}/models/${encodedModelId}`,
      token,
    ),
  ]);
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

export async function deleteEntry(
  token: string,
  entryId: string,
): Promise<void> {
  await deleteBackend(`/v1/entries/${encodeURIComponent(entryId)}`, token);
}

export async function deleteModel(
  token: string,
  entryId: string,
  modelId: string,
): Promise<void> {
  await deleteBackend(
    `/v1/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(
      modelId,
    )}`,
    token,
  );
}

async function fetchModelGraph(
  token: string | undefined,
  entryId: string,
  model: Model,
): Promise<EntryGraph> {
  const response = await fetchBackend<ModelArtifactListResponse>(
    `/v1/entries/${encodeURIComponent(entryId)}/models/${encodeURIComponent(
      model.id,
    )}/artifacts`,
    token,
  );
  return modelGraphFromBackend(entryId, model, response);
}

function createEntryRequest(input: CreateEntryInput): BackendCreateEntryRequest {
  const entities = input.entities ?? [];
  return {
    id: input.id,
    name: input.name,
    description: input.description,
    thumbnail_image_url: input.thumbnail_image_url,
    metadata: entryMetadataRequest(input.metadata),
    artifacts: entities.filter(isArtifactEntity).map(createArtifactRequest),
    models: (input.models ?? []).map(createModelRequest),
  };
}

/**
 * Shapes the entry's own facts the way the record stores them. Empty fields
 * are omitted rather than sent as null: the backend decodes into a struct of
 * pointers, and an explicit null is indistinguishable from "unset" once it
 * lands, so leaving the key out keeps the stored object honest.
 */
function entryMetadataRequest(metadata?: CreateEntryMetadata): JSONRecord {
  if (!metadata) {
    return {};
  }
  const result: JSONRecord = {};
  const pdb = stringOrNull(metadata.pdb);
  if (pdb) {
    result.external_refs = { pdb: pdb.toUpperCase() };
  }
  const resolution = numberOrNull(metadata.resolution);
  if (resolution !== null) {
    result.resolution = resolution;
  }
  const organism = stringOrNull(metadata.organism);
  if (organism) {
    result.organism = organism;
  }
  const method = stringOrNull(metadata.method);
  if (method) {
    result.method = method;
  }
  const spaceGroup = stringOrNull(metadata.space_group);
  if (spaceGroup) {
    result.space_group = spaceGroup;
  }
  return result;
}

function createModelRequest(input: CreateModelInput): BackendCreateModelRequest {
  const entities = input.entities ?? [];
  const relations = input.relations ?? [];
  const artifacts = entities.filter(isArtifactEntity).map(createArtifactRequest);
  const primaryModelEntity =
    entities.find((entity) => entity.type === "model") ?? null;

  return {
    id: input.id,
    name: input.name,
    description: input.description,
    thumbnail_image_url: input.thumbnail_image_url,
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
    uri: stringOrNull(payload.file_url),
    sha256: stringOrNull(payload.sha256),
    format: artifactFormat(entity),
    size_bytes: numberOrNull(payload.size),
    metadata,
  };
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
  const payload: JSONRecord = {
    ...(artifact.uri ? { file_url: artifact.uri } : {}),
    ...(artifact.size_bytes != null ? { size: artifact.size_bytes } : {}),
    ...(entityType === "data" && artifact.format
      ? { type: artifact.format }
      : {}),
    ...(Object.keys(artifact.metadata ?? {}).length > 0
      ? { metadata: artifact.metadata }
      : {}),
  };
  if (entityType === "model") {
    const metadata = objectRecord(model?.metadata);
    const authors = stringArray(metadata.authors);
    const affiliation = stringOrNull(metadata.affiliation);
    if (authors.length > 0) {
      payload.authors = authors;
    }
    if (affiliation) {
      payload.affiliation = affiliation;
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
  if (model?.primary_artifact_id === artifact.id) {
    return "model";
  }
  return artifact.format === "pdb" || artifact.format === "mmcif"
    ? "model"
    : "data";
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

function modelMetadataFromEntity(entity: CreateEntityInput | null): JSONRecord {
  if (!entity) {
    return {};
  }
  const payload = objectRecord(entity.payload);
  const metadata: JSONRecord = {};
  const authors = stringArray(payload.authors);
  const affiliation = stringOrNull(payload.affiliation);
  if (authors.length > 0) {
    metadata.authors = authors;
  }
  if (affiliation) {
    metadata.affiliation = affiliation;
  }

  // Composition was read out of the coordinates and parked on the artifact.
  // The pages show it as a property of the model, so it is copied across
  // rather than left where only a file preview would find it.
  const parsed = objectRecord(payload.metadata);
  for (const key of [
    "atom_count",
    "modeled_residues",
    "unique_protein_chains",
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

async function deleteBackend(path: string, token: string): Promise<void> {
  const baseUrl = getApiBaseUrl().replace(/\/+$/, "");

  let response: Response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
      method: "DELETE",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${token}`,
      },
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
}

async function fetchBackend<T>(path: string, token?: string): Promise<T> {
  const baseUrl = getApiBaseUrl().replace(/\/+$/, "");

  const headers: Record<string, string> = { Accept: "application/json" };
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
