"use client";

import Link from "next/link";
import { useMemo, useState } from "react";

import type {
  Entity,
  EntityRelation,
  MetricsPayload,
  Model,
} from "@/lib/api/entries";

import styles from "@/app/entries/[entryId]/entry-page.module.css";

type MetricKey = keyof MetricsPayload;

type SortField = "name" | "date" | MetricKey;

type SortState = {
  field: SortField;
  asc: boolean;
};

const metricFields: {
  key: MetricKey;
  label: string;
  lowerBetter: boolean;
}[] = [
  { key: "r_work", label: "R-work", lowerBetter: true },
  { key: "r_free", label: "R-free", lowerBetter: true },
  { key: "cc", label: "CC", lowerBetter: false },
  { key: "rscc", label: "RSCC", lowerBetter: false },
];

const metricByKey = new Map(metricFields.map((field) => [field.key, field]));

const numberFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 3,
});

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
});

function defaultAscending(field: SortField): boolean {
  if (field === "name") {
    return true;
  }
  if (field === "date") {
    return false;
  }
  return metricByKey.get(field)?.lowerBetter ?? true;
}

/**
 * Merge the metrics that belong to each model.
 *
 * Every model owns a `model`-type entity (linked by `model_id`). Metrics live
 * in separate `metrics`-type entities connected to that model entity via a
 * `metrics_for` relation. We resolve those relations client-side so the list
 * can sort by metric without any backend changes.
 */
function buildModelMetrics(
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

export default function SortableModelList({
  entryId,
  models,
  entities,
  relations,
}: {
  entryId: string;
  models: Model[];
  entities: Entity[];
  relations: EntityRelation[];
}) {
  const metricsByModelId = useMemo(
    () => buildModelMetrics(entities, relations),
    [entities, relations],
  );

  const [sort, setSort] = useState<SortState>({ field: "name", asc: true });

  const sortedModels = useMemo(() => {
    const withMetrics = models.map((model) => ({
      model,
      metrics: metricsByModelId.get(model.id) ?? {},
    }));

    const { field, asc } = sort;
    const direction = asc ? 1 : -1;

    return withMetrics
      .slice()
      .sort((a, b) => {
        if (field === "name") {
          return direction * a.model.name.localeCompare(b.model.name);
        }
        if (field === "date") {
          const at = new Date(a.model.created_at).getTime();
          const bt = new Date(b.model.created_at).getTime();
          return direction * (at - bt);
        }

        const av = a.metrics[field];
        const bv = b.metrics[field];
        const aHas = typeof av === "number";
        const bHas = typeof bv === "number";
        // Models missing this metric always sink to the bottom.
        if (!aHas && !bHas) {
          return a.model.name.localeCompare(b.model.name);
        }
        if (!aHas) {
          return 1;
        }
        if (!bHas) {
          return -1;
        }
        return direction * (av - bv);
      })
      .map((row) => row.model);
  }, [models, metricsByModelId, sort]);

  if (models.length === 0) {
    return <p className={styles.emptyState}>No models.</p>;
  }

  const handleFieldChange = (field: SortField) => {
    setSort({ field, asc: defaultAscending(field) });
  };

  return (
    <div>
      <div className={styles.sortToolbar}>
        <label className={styles.sortLabel} htmlFor="model-sort-field">
          Sort by
        </label>
        <select
          id="model-sort-field"
          className={styles.sortSelect}
          value={sort.field}
          onChange={(event) =>
            handleFieldChange(event.target.value as SortField)
          }
        >
          <option value="name">Name</option>
          <option value="date">Date</option>
          <optgroup label="Metrics">
            {metricFields.map((metric) => (
              <option key={metric.key} value={metric.key}>
                {metric.label}
              </option>
            ))}
          </optgroup>
        </select>
        <button
          type="button"
          className={styles.sortDirection}
          aria-label={
            sort.asc ? "Sorted ascending" : "Sorted descending"
          }
          aria-pressed={!sort.asc}
          onClick={() => setSort((prev) => ({ ...prev, asc: !prev.asc }))}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
            style={{
              transform: sort.asc ? "none" : "rotate(180deg)",
              transition: "transform 120ms ease",
            }}
          >
            <path d="M12 19V5M5 12l7-7 7 7" />
          </svg>
        </button>
      </div>

      <div className={styles.modelRows}>
        {sortedModels.map((model) => {
          const metrics = metricsByModelId.get(model.id) ?? {};
          return (
            <Link
              key={model.id}
              className={styles.modelRow}
              href={`/entries/${entryId}/models/${model.id}`}
            >
              <span
                className={styles.modelRowThumb}
                data-empty={model.thumbnail_image_url ? undefined : "true"}
              >
                {model.thumbnail_image_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={model.thumbnail_image_url} alt="" loading="lazy" />
                ) : (
                  <PlaceholderIcon />
                )}
              </span>

              <span className={styles.modelRowMain}>
                <span className={styles.modelRowName}>{model.name}</span>
                <span className={styles.modelRowDate}>
                  {formatDate(model.created_at)}
                </span>
              </span>

              <span className={styles.modelRowMetrics}>
                {metricFields.map((metric) => {
                  const value = metrics[metric.key];
                  const isActive = sort.field === metric.key;
                  return (
                    <span
                      key={metric.key}
                      className={styles.modelRowMetric}
                      data-active={isActive ? "true" : undefined}
                    >
                      <span className={styles.modelRowMetricLabel}>
                        {metric.label}
                      </span>
                      <b className={styles.modelRowMetricValue}>
                        {typeof value === "number"
                          ? numberFormatter.format(value)
                          : "—"}
                      </b>
                    </span>
                  );
                })}
              </span>

              <svg
                className={styles.modelRowChevron}
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="m9 18 6-6-6-6" />
              </svg>
            </Link>
          );
        })}
      </div>
    </div>
  );
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return dateFormatter.format(date);
}

function PlaceholderIcon() {
  return (
    <svg width="26" height="26" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.4" fill="currentColor" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(120 12 12)" />
    </svg>
  );
}
