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

export type MetricStatus = "good" | "warn" | "bad";

export type MetricSpec = {
  key: keyof MetricsPayload;
  label: string;
  /** What the rail's own column head says, where the room is 86px. */
  short: string;
  /** R-factors and clash counts fall; correlation coefficients rise. */
  lowerIsBetter: boolean;
  decimals: number;
  /**
   * The fixed track a value is read against: what the poor end of the scale
   * stands for, and what the good end does.
   *
   * Fixed on purpose. A track stretched to fit whatever an entry happens to
   * hold makes a thousandth of a difference fill the page on one entry and a
   * tenth fill it on the next, so the same picture means something different
   * every time. Against a fixed track a dot near the good end means the model
   * is good, full stop, and two entries can be read against each other.
   */
  worstEnd: number;
  bestEnd: number;
  /**
   * The two cuts in that track, from the poor end: poor|acceptable first,
   * acceptable|good second.
   */
  breaks: [number, number];
  status: (value: number) => MetricStatus;
};

/**
 * Every figure a model can be ordered by, in the order they are shown.
 *
 * The thresholds are the conventional ones the model page already colours its
 * tiles with, so a value that reads as bad on the record reads as bad in the
 * rail and lands in the red band of its scale. They are fixed values and not
 * percentiles of the archive: a percentile needs the distribution over every
 * structure ever deposited, which is not something we hold.
 *
 * The ends are set wide enough to take the values that actually occur and no
 * wider -- an R-free axis running to 1.0 would squeeze every real model into
 * its first third. Anything outside them is drawn at the end it ran off.
 */
export const metricSpecs: MetricSpec[] = [
  graded({
    key: "r_work",
    label: "R-work",
    short: "R-work",
    lowerIsBetter: true,
    decimals: 3,
    worstEnd: 0.4,
    bestEnd: 0.1,
    breaks: [0.3, 0.25],
  }),
  graded({
    key: "r_free",
    label: "R-free",
    short: "R-free",
    lowerIsBetter: true,
    decimals: 3,
    worstEnd: 0.4,
    bestEnd: 0.1,
    breaks: [0.3, 0.25],
  }),
  graded({
    key: "rscc",
    label: "RSCC",
    short: "RSCC",
    lowerIsBetter: false,
    decimals: 3,
    worstEnd: 0.6,
    bestEnd: 1,
    breaks: [0.8, 0.9],
  }),
  graded({
    key: "clashscore",
    label: "Clashscore",
    short: "Clash",
    lowerIsBetter: true,
    decimals: 1,
    worstEnd: 30,
    bestEnd: 0,
    breaks: [20, 10],
  }),
  graded({
    key: "molprobity_score",
    label: "MolProbity score",
    short: "MolProb",
    lowerIsBetter: true,
    decimals: 2,
    worstEnd: 4,
    bestEnd: 0,
    breaks: [3, 2],
  }),
];

/**
 * One spec with its verdict read off its own thresholds.
 *
 * Derived rather than written twice so the band a reader sees a dot standing
 * in and the colour that same value is given elsewhere cannot drift apart.
 */
function graded(spec: Omit<MetricSpec, "status">): MetricSpec {
  const [poor, good] = spec.breaks;
  return {
    ...spec,
    status: (value) =>
      spec.lowerIsBetter
        ? value < good
          ? "good"
          : value < poor
            ? "warn"
            : "bad"
        : value >= good
          ? "good"
          : value >= poor
            ? "warn"
            : "bad",
  };
}

const specByKey = new Map(metricSpecs.map((spec) => [String(spec.key), spec]));

/** The spec for a key that arrived from a URL, or null if it is not one. */
export function metricSpec(key: string | null | undefined): MetricSpec | null {
  return key ? (specByKey.get(key) ?? null) : null;
}

export function formatMetric(spec: MetricSpec, value: number): string {
  return value.toFixed(spec.decimals);
}

/**
 * Models ordered by one figure, with the ones that do not carry it at the
 * bottom.
 *
 * A predicted model has no R-factors and a model refined without a map has no
 * RSCC, so a missing value is the normal case rather than an error. Sinking
 * them keeps the top of the list answering the question that was asked; sorting
 * them as zero would put every one of them first under "lower is better".
 */
export function sortModelsByMetric<T extends { metrics: MetricsPayload }>(
  models: T[],
  spec: MetricSpec,
  ascending: boolean,
): T[] {
  const direction = ascending ? 1 : -1;
  return models.slice().sort((first, second) => {
    const a = first.metrics[spec.key];
    const b = second.metrics[spec.key];
    const aHas = typeof a === "number";
    const bHas = typeof b === "number";
    if (!aHas && !bHas) {
      return 0;
    }
    if (!aHas) {
      return 1;
    }
    if (!bHas) {
      return -1;
    }
    return direction * (a - b);
  });
}

/** The best value in each column, for the mark that points at it. */
export function bestMetricValues(
  models: { metrics: MetricsPayload }[],
): Map<keyof MetricsPayload, number> {
  const best = new Map<keyof MetricsPayload, number>();
  for (const spec of metricSpecs) {
    const values = models
      .map((model) => model.metrics[spec.key])
      .filter((value): value is number => typeof value === "number");
    if (values.length > 0) {
      best.set(
        spec.key,
        spec.lowerIsBetter ? Math.min(...values) : Math.max(...values),
      );
    }
  }
  return best;
}
