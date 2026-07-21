import "server-only";

import { getApiBaseUrl } from "./baseUrl";

export const demoEntryId = "d1a642ca-cbe0-4210-8108-2d1222e28984";
export const demoUserId = "3b696db1-f943-4e44-b271-8cbdd36e7fc1";

export type Entry = {
  id: string;
  name: string;
  description: string | null;
  thumbnail_image_url: string | null;
  created_at: string;
  updated_at: string;
};

export type Experiment = {
  id: string;
  entry_id: string;
  name: string;
  description: string | null;
  thumbnail_image_url: string | null;
  created_at: string;
  updated_at: string;
};

export type EntityType = "data" | "metrics" | "model" | "program";
export type EntityLevel = "L0" | "L1" | "L2" | "L3";

export type FastaMetadata = {
  length?: number;
  chains?: number;
  sequence?: string;
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
  experiment_id: string | null;
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
  experiments: Experiment[];
  entities: Entity[];
  relations: EntityRelation[];
};

export type ExperimentPageData = {
  entry: Entry;
  experiment: Experiment;
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

export type CreateExperimentInput = {
  name: string;
  description?: string | null;
  thumbnail_image_url?: string | null;
  entities?: CreateEntityInput[];
  relations?: CreateEntityRelationInput[];
};

export type CreateEntryInput = {
  name: string;
  description?: string | null;
  thumbnail_image_url?: string | null;
  entities?: CreateEntityInput[];
  experiments?: CreateExperimentInput[];
};

export async function createEntry(
  token: string,
  input: CreateEntryInput,
): Promise<void> {
  const baseUrl = getApiBaseUrl().replace(/\/+$/, "");
  let response: Response;
  try {
    response = await fetch(`${baseUrl}/v1/entries`, {
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
  token: string,
  entryId: string = demoEntryId,
): Promise<EntryPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const [entry, experiments, entityGraph] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<ListResponse<Experiment>>(
      `/v1/entries/${encodedEntryId}/experiments`,
      token,
    ),
    fetchBackend<EntityListResponse>(
      `/v1/entries/${encodedEntryId}/entities`,
      token,
    ),
  ]);

  return {
    entry,
    experiments: experiments.items,
    entities: entityGraph.items,
    relations: entityGraph.relations,
  };
}

export async function getExperimentPageData(
  token: string,
  entryId: string,
  experimentId: string,
): Promise<ExperimentPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const encodedExperimentId = encodeURIComponent(experimentId);
  const [entry, experiment, entityGraph] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<Experiment>(
      `/v1/entries/${encodedEntryId}/experiments/${encodedExperimentId}`,
      token,
    ),
    fetchBackend<EntityListResponse>(
      `/v1/entries/${encodedEntryId}/entities`,
      token,
    ),
  ]);

  const scopedEntities = entityGraph.items.filter(
    (entity) =>
      entity.experiment_id === experiment.id ||
      (entity.experiment_id === null && entity.level === "L0"),
  );
  const scopedIds = new Set(scopedEntities.map((entity) => entity.id));

  return {
    entry,
    experiment,
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
  token: string,
  entryId: string,
): Promise<EntryGraph> {
  const encodedEntryId = encodeURIComponent(entryId);
  const entityGraph = await fetchBackend<EntityListResponse>(
    `/v1/entries/${encodedEntryId}/entities`,
    token,
  );
  return { entities: entityGraph.items, relations: entityGraph.relations };
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
