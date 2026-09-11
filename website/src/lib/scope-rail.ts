import type {
  Entity,
  EntityRelation,
  EntryPageData,
  MetricsPayload,
  ProgramPayload,
} from "@/lib/api/entries";
import { buildModelMetrics, modelTitle } from "@/lib/model-metrics";

/**
 * One row of the scope rail.
 *
 * The rail used to carry a name and nothing else, on the grounds that the
 * numbers telling models apart live on each model's own page. That is exactly
 * what made choosing between six models a tour of six pages, so the figures
 * come with the list now: closed, the rail still shows only the name, and the
 * rest is what it opens into.
 */
export type ScopeRailModel = {
  id: string;
  title: string;
  thumbnailImageURL: string | null;
  metrics: MetricsPayload;
  /** The program that produced the model, with its version, or null. */
  software: string | null;
  createdAt: string;
};

/**
 * The rail's model list, in the order the backend returned -- which is the
 * order they were made in, and that order is itself information. Sorting is
 * the reader's to ask for.
 *
 * Built here rather than on each page so the entry and the model render the
 * same list from the same data.
 */
export function scopeRailModels(data: EntryPageData): ScopeRailModel[] {
  const metricsByModelId = buildModelMetrics(data.entities, data.relations);
  const softwareByModelId = modelSoftware(data.entities, data.relations);

  return data.models.map((model) => ({
    id: model.id,
    title: modelTitle(model),
    thumbnailImageURL:
      model.thumbnail_image_url?.trim() ||
      data.entry.thumbnail_image_url?.trim() ||
      null,
    metrics: metricsByModelId.get(model.id) ?? {},
    software: softwareByModelId.get(model.id) ?? null,
    createdAt: model.created_at,
  }));
}

/**
 * What each model was made with, read the way the pipeline graph reads it: the
 * model entity is an `output_of` its program, and the program's payload
 * carries the name and version.
 *
 * A model built by hand, or deposited without its provenance recorded, has no
 * program and simply gets no line.
 */
function modelSoftware(
  entities: Entity[],
  relations: EntityRelation[],
): Map<string, string> {
  const entityById = new Map(entities.map((entity) => [entity.id, entity]));
  const software = new Map<string, string>();

  for (const entity of entities) {
    if (entity.type !== "model" || !entity.model_id) {
      continue;
    }
    for (const relation of relations) {
      if (
        relation.source_entity_id !== entity.id ||
        relation.relation_type !== "output_of"
      ) {
        continue;
      }
      const program = entityById.get(relation.target_entity_id);
      if (program?.type !== "program") {
        continue;
      }
      const payload = program.payload as ProgramPayload;
      const name = payload.name?.trim();
      if (!name) {
        continue;
      }
      const version = payload.version?.trim();
      software.set(entity.model_id, version ? `${name} ${version}` : name);
      break;
    }
  }

  return software;
}
