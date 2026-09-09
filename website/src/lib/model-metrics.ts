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
/** What to call a model. A model with no title of its own is rare -- the
 *  depositor names them -- and "Model" is what the rail has always shown, so
 *  the heading and the rail row agree whatever the data holds. */
export function modelTitle(model: { title: string | null }): string {
  return model.title?.trim() || "Model";
}

export function defaultModel(models: Model[]): Model | null {
  const deposited = models.find((model) =>
    (model.title ?? "").toLowerCase().includes("deposited"),
  );
  return deposited ?? models[0] ?? null;
}

/**
 * The names a metrics payload's figures go by, in the order they are shown.
 *
 * One list rather than one per view: the dialog prints these names and the CSV
 * it downloads carries the same ones, so the file and the screen agree.
 */
export const metricLabels: { key: keyof MetricsPayload; label: string }[] = [
  { key: "r_work", label: "R-work" },
  { key: "r_free", label: "R-free" },
  { key: "clashscore", label: "Clashscore" },
  { key: "molprobity_score", label: "MolProbity score" },
  { key: "rscc", label: "RSCC" },
];

/**
 * A metrics record as a two-column CSV: the name of each figure and its value.
 *
 * Metrics are not a file -- there is nothing on object storage to link to --
 * so the only way to hand them over is to assemble one here. Every number the
 * record holds goes in, the known ones first under the names the dialog shows
 * and anything else after them under its own key: a file you take away should
 * not quietly drop what was recorded.
 *
 * Values are written as they are stored, not as they are displayed. The screen
 * rounds to three decimals and groups thousands with a comma, and a comma in a
 * CSV field is a new column.
 */
export function metricsCSV(payload: Record<string, unknown>): string {
  const known = new Set<string>(metricLabels.map((metric) => String(metric.key)));
  const rows: [string, string][] = [];

  for (const metric of metricLabels) {
    const value = payload[metric.key as string];
    if (typeof value === "number" && Number.isFinite(value)) {
      rows.push([metric.label, String(value)]);
    }
  }

  for (const [key, value] of Object.entries(payload)) {
    if (known.has(key) || key === "cc") {
      continue;
    }
    if (typeof value === "number" && Number.isFinite(value)) {
      rows.push([key, String(value)]);
    } else if (typeof value === "string" && value.trim() !== "") {
      rows.push([key, value.trim()]);
    }
  }

  // CRLF and a header row, which is what a spreadsheet expects to open.
  return [["Metric", "Value"], ...rows]
    .map((row) => row.map(field).join(","))
    .join("\r\n");
}

// Anything that would otherwise be read as structure -- a separator, a quote,
// a line break -- puts the field in quotes, and a quote inside it is doubled.
function field(value: string): string {
  return /[",\r\n]|^\s|\s$/.test(value)
    ? `"${value.replace(/"/g, '""')}"`
    : value;
}

/** A name a browser will accept as a download, ending in .csv exactly once. */
export function csvFileName(name: string): string {
  const base = name.trim().replace(/\.csv$/i, "").replace(/[\\/:*?"<>|]+/g, "-");
  return `${base || "metrics"}.csv`;
}
