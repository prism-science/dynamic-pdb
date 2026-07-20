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

// Payloads for data/model are passed through the API verbatim, so metadata is
// open-ended and grows per file type. `type` is the concrete data subtype
// (fasta, mtz, pdb, ...); `metadata` carries the parsed, per-type detail.
export type DataPayload = {
  file_url: string;
  type?: string;
  size?: number;
  metadata?: Record<string, unknown>;
};

export type ModelPayload = {
  file_url: string;
  type?: string;
  size?: number;
  metadata?: Record<string, unknown>;
};

export type MetricsPayload = {
  r_free?: number;
  r_work?: number;
  rscc?: number;
  cc?: number;
  clashscore?: number;
  ramachandran_outlier_percent?: number;
  side_chain_outlier_percent?: number;
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
  | "processed_from"
  | "generated_from"
  | "refined_from"
  | "evaluates";

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

export async function listEntries(token: string): Promise<Entry[]> {
  const response = await fetchBackend<ListResponse<Entry>>("/v1/entries", token);
  return response.items;
}

export async function getEntryPageData(
  token: string,
  entryId: string = demoEntryId,
): Promise<EntryPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const [entry, experiments, entities, relations] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<ListResponse<Experiment>>(
      `/v1/entries/${encodedEntryId}/experiments`,
      token,
    ),
    fetchBackend<ListResponse<Entity>>(
      `/v1/entries/${encodedEntryId}/entities`,
      token,
    ),
    listRelations(token, entryId),
  ]);

  return {
    entry,
    experiments: experiments.items,
    entities: entities.items,
    relations,
  };
}

export async function getExperimentPageData(
  token: string,
  entryId: string,
  experimentId: string,
): Promise<ExperimentPageData> {
  const encodedEntryId = encodeURIComponent(entryId);
  const encodedExperimentId = encodeURIComponent(experimentId);
  const [entry, experiment, entities, relations] = await Promise.all([
    fetchBackend<Entry>(`/v1/entries/${encodedEntryId}`, token),
    fetchBackend<Experiment>(
      `/v1/entries/${encodedEntryId}/experiments/${encodedExperimentId}`,
      token,
    ),
    fetchBackend<ListResponse<Entity>>(
      `/v1/entries/${encodedEntryId}/entities`,
      token,
    ),
    listRelations(token, entryId),
  ]);

  const scopedEntities = entities.items.filter(
    (entity) =>
      entity.experiment_id === experiment.id ||
      (entity.experiment_id === null && entity.level === "L0"),
  );
  const scopedIds = new Set(scopedEntities.map((entity) => entity.id));

  return {
    entry,
    experiment,
    entities: scopedEntities,
    relations: relations.filter(
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

// Full provenance graph for an entry: every entity plus every relation.
export async function getEntryGraph(
  token: string,
  entryId: string,
): Promise<EntryGraph> {
  const encodedEntryId = encodeURIComponent(entryId);
  const [entities, relations] = await Promise.all([
    fetchBackend<ListResponse<Entity>>(
      `/v1/entries/${encodedEntryId}/entities`,
      token,
    ),
    listRelations(token, entryId),
  ]);
  return { entities: entities.items, relations };
}

// listRelations degrades gracefully: if the backend build predates the
// provenance endpoint (or it is otherwise unavailable), we return no edges
// rather than failing the whole page.
async function listRelations(
  token: string,
  entryId: string,
): Promise<EntityRelation[]> {
  try {
    const response = await fetchBackend<ListResponse<EntityRelation>>(
      `/v1/entries/${encodeURIComponent(entryId)}/relations`,
      token,
    );
    return response.items;
  } catch {
    return [];
  }
}

async function fetchBackend<T>(path: string, token: string): Promise<T> {
  const baseUrl = getApiBaseUrl().replace(/\/+$/, "");

  let response: Response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
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

  return (await response.json()) as T;
}
