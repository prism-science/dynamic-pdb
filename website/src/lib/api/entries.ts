import "server-only";

import { getApiBaseUrl } from "./baseUrl";

export type Entry = {
  id: string;
  created_by: string;
  name: string;
  description: string | null;
  thumbnail_image_url: string | null;
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

export type DataPayload = {
  file_url: string;
  type?: string;
  authors?: string[];
  affiliation?: string | null;
  size?: number;
  metadata?: Record<string, unknown>;
};

export type ModelPayload = {
  file_url: string;
  authors?: string[];
  affiliation?: string | null;
  size?: number;
  metadata?: Record<string, unknown>;
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

export type RelationType =
  | "input_to"
  | "output_of"
  | "metrics_for";

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

type EntityListResponse = {
  items: Entity[];
  relations: EntityRelation[];
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
  payload: Record<string, unknown>;
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
};

export type CreateEntryInput = {
  id?: string;
  name: string;
  description?: string | null;
  thumbnail_image_url?: string | null;
  entities?: CreateEntityInput[];
  models?: CreateModelInput[];
};

export async function createModel(
  token: string,
  entryId: string,
  input: CreateModelInput,
): Promise<void> {
  await postJSON(
    `/v1/entries/${encodeURIComponent(entryId)}/models`,
    token,
    input,
  );
}

export async function createEntry(
  token: string,
  input: CreateEntryInput,
): Promise<void> {
  await postJSON("/v1/entries", token, input);
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
  const [entry, models, entityGraph] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<ListResponse<Model>>(
      `/v1/entries/${encodedEntryId}/models`,
      token,
    ),
    fetchBackend<EntityListResponse>(
      `/v1/entries/${encodedEntryId}/entities`,
      token,
    ),
  ]);

  return {
    entry,
    models: models.items,
    entities: entityGraph.items,
    relations: entityGraph.relations,
  };
}

export async function getModelPageData(
  token: string | undefined,
  entryId: string,
  modelId: string,
): Promise<ModelPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const encodedModelId = encodeURIComponent(modelId);
  const [entry, model, entityGraph] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<Model>(
      `/v1/entries/${encodedEntryId}/models/${encodedModelId}`,
      token,
    ),
    fetchBackend<EntityListResponse>(
      `/v1/entries/${encodedEntryId}/entities`,
      token,
    ),
  ]);

  // Only what this model owns. Structure-level entities (the FASTA and other
  // entry-wide L0 data) live on the entry page and are not repeated here.
  const scopedEntities = entityGraph.items.filter(
    (entity) => entity.model_id === model.id,
  );
  const scopedIds = new Set(scopedEntities.map((entity) => entity.id));

  return {
    entry,
    model,
    entities: scopedEntities,
    relations: entityGraph.relations.filter(
      (relation) =>
        scopedIds.has(relation.source_entity_id) ||
        scopedIds.has(relation.target_entity_id),
    ),
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
  const encodedEntryId = encodeURIComponent(entryId);
  const entityGraph = await fetchBackend<EntityListResponse>(
    `/v1/entries/${encodedEntryId}/entities`,
    token,
  );
  return { entities: entityGraph.items, relations: entityGraph.relations };
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
