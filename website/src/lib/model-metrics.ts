import type {
  Entity,
  EntityRelation,
  MetricsPayload,
  Model,
} from "@/lib/api/entries";

/**
 * Merge the metrics that belong to each model.
 *
 * Every model owns a `model`-type entity (linked by `model_id`). Metrics live
 * in separate `metrics`-type entities connected to that model entity via a
 * `metrics_for` relation. Resolving those relations here lets both the model
 * table and the entry overview read a model's numbers without any backend
 * changes.
 */
export function buildModelMetrics(
  entities: Entity[],
  relations: EntityRelation[],
): Map<string, MetricsPayload> {
  const entityById = new Map(entities.map((entity) => [entity.id, entity]));

  const modelEntityByModelId = new Map<string, Entity>();
  for (const entity of entities) {
    if (entity.type === "model" && entity.model_id) {
      modelEntityByModelId.set(entity.model_id, entity);
    }
  }

  const metricsByModelEntityId = new Map<string, MetricsPayload>();
  for (const relation of relations) {
    if (relation.relation_type !== "metrics_for") {
      continue;
    }
    const source = entityById.get(relation.source_entity_id);
    if (source?.type !== "metrics") {
      continue;
    }
    const merged = metricsByModelEntityId.get(relation.target_entity_id) ?? {};
    Object.assign(merged, source.payload as MetricsPayload);
    metricsByModelEntityId.set(relation.target_entity_id, merged);
  }

  const byModelId = new Map<string, MetricsPayload>();
  for (const [modelId, entity] of modelEntityByModelId) {
    const metrics = metricsByModelEntityId.get(entity.id);
    if (metrics) {
      byModelId.set(modelId, metrics);
    }
  }
  return byModelId;
}

/**
 * The model the entry leads with: the deposited one, or the first the backend
 * returned.
 *
 * Matched on the title because nothing marks a model as the deposit. The names
 * already vary -- "Deposited model", "Deposited coordinate model" -- so this is
 * a substring test rather than an equality one, and it still misses a deposit
 * whose depositor named it something else. A `role` or `is_default` field on the
 * model would replace this outright and turn "first returned" back into a
 * fallback instead of the common path.
 */
export function defaultModel(models: Model[]): Model | null {
  const deposited = models.find((model) =>
    (model.title ?? "").toLowerCase().includes("deposited"),
  );
  return deposited ?? models[0] ?? null;
}
