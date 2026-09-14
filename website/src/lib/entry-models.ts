import type {
  Entity,
  EntityRelation,
  EntryPageData,
  MetricsPayload,
  ProgramPayload,
} from "@/lib/api/entries";
import { buildModelMetrics, modelTitle } from "@/lib/model-metrics";

/**
 * One model of an entry, with the few things every list of models needs.
 *
 * The figures travel with the name on purpose. They used to live only on each
 * model's own page, which made choosing between six models a tour of six
 * pages; carrying them here is what lets the strip under the tabs be both the
 * navigation and the comparison.
 */
export type EntryModel = {
  id: string;
  title: string;
  thumbnailImageURL: string | null;
  metrics: MetricsPayload;
  /** The program that produced the model, with its version, or null. */
  software: string | null;
  createdAt: string;
};

/**
 * The entry's models, in the order the backend returned -- which is the order
 * they were made in, and that order is itself information. It is also what the
 * model colours are pinned to, so it must not be re-sorted here.
 *
 * Built here rather than on each page so every view of the entry renders the
 * same list from the same data.
 */
export function entryModels(data: EntryPageData): EntryModel[] {
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
