"use client";

import { useLayoutEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import type {
  Entity,
  EntityRelation,
  MetricsPayload,
  Model,
} from "@/lib/api/entries";

import styles from "@/app/entries/[entryId]/entry-page.module.css";

type MetricKey = keyof MetricsPayload;

type MetricStatus = "good" | "warn" | "bad";

type SortState = {
  key: MetricKey;
  asc: boolean;
} | null;

const metricColumns: {
  key: MetricKey;
  label: string;
  lowerBetter: boolean;
  status: (value: number) => MetricStatus;
}[] = [
  {
    key: "r_work",
    label: "R-work",
    lowerBetter: true,
    status: (v) => (v < 0.25 ? "good" : v < 0.3 ? "warn" : "bad"),
  },
  {
    key: "r_free",
    label: "R-free",
    lowerBetter: true,
    status: (v) => (v < 0.25 ? "good" : v < 0.3 ? "warn" : "bad"),
  },
  {
    key: "cc",
    label: "CC",
    lowerBetter: false,
    status: (v) => (v >= 0.9 ? "good" : v >= 0.8 ? "warn" : "bad"),
  },
  {
    key: "rscc",
    label: "RSCC",
    lowerBetter: false,
    status: (v) => (v >= 0.9 ? "good" : v >= 0.8 ? "warn" : "bad"),
  },
];

const metricByKey = new Map(metricColumns.map((column) => [column.key, column]));

const numberFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 3,
});

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
});

/**
 * Merge the metrics that belong to each model.
 *
 * Every model owns a `model`-type entity (linked by `model_id`). Metrics live
 * in separate `metrics`-type entities connected to that model entity via a
 * `metrics_for` relation. We resolve those relations client-side so the table
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
  const router = useRouter();

  const metricsByModelId = useMemo(
    () => buildModelMetrics(entities, relations),
    [entities, relations],
  );

  // No default sort: models render in the order the backend returned them
  // until the user picks a column.
  const [sort, setSort] = useState<SortState>(null);

  const bestByColumn = useMemo(() => {
    const best = new Map<MetricKey, number>();
    for (const column of metricColumns) {
      const values = models
        .map((model) => metricsByModelId.get(model.id)?.[column.key])
        .filter((value): value is number => typeof value === "number");
      if (values.length > 0) {
        best.set(
          column.key,
          column.lowerBetter ? Math.min(...values) : Math.max(...values),
        );
      }
    }
    return best;
  }, [models, metricsByModelId]);

  const sortedModels = useMemo(() => {
    if (!sort) {
      return models;
    }
    const { key, asc } = sort;
    const direction = asc ? 1 : -1;
    return models.slice().sort((a, b) => {
      const av = metricsByModelId.get(a.id)?.[key];
      const bv = metricsByModelId.get(b.id)?.[key];
      const aHas = typeof av === "number";
      const bHas = typeof bv === "number";
      // Models missing this metric always sink to the bottom.
      if (!aHas && !bHas) {
        return 0;
      }
      if (!aHas) {
        return 1;
      }
      if (!bHas) {
        return -1;
      }
      return direction * (av - bv);
    });
  }, [models, metricsByModelId, sort]);

  // FLIP animation: slide rows from their previous position to the new one
  // whenever the sort order changes, so the reorder is visible.
  const rowRefs = useRef<Map<string, HTMLTableRowElement>>(new Map());
  const prevRects = useRef<Map<string, number>>(new Map());
  useLayoutEffect(() => {
    rowRefs.current.forEach((node, id) => {
      const next = node.getBoundingClientRect().top;
      const prev = prevRects.current.get(id);
      if (prev !== undefined && prev !== next) {
        const delta = prev - next;
        node.style.transform = `translateY(${delta}px)`;
        node.style.transition = "none";
        requestAnimationFrame(() => {
          node.style.transition = "transform 320ms cubic-bezier(0.2, 0.7, 0.3, 1)";
          node.style.transform = "";
        });
      }
      prevRects.current.set(id, next);
    });
  }, [sortedModels]);

  if (models.length === 0) {
    return <p className={styles.emptyState}>No models.</p>;
  }

  const handleSort = (key: MetricKey) => {
    setSort((prev) => {
      if (prev?.key === key) {
        return { key, asc: !prev.asc };
      }
      const column = metricByKey.get(key);
      return { key, asc: column?.lowerBetter ?? true };
    });
  };

  return (
    <div className={styles.modelTableWrap}>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th scope="col" className={styles.modelTableModelHead}>
              Model
            </th>
            {metricColumns.map((column) => {
              const active = sort?.key === column.key;
              return (
                <th
                  key={column.key}
                  scope="col"
                  className={styles.modelTableMetricHead}
                  data-active={active ? "true" : undefined}
                  aria-sort={
                    active ? (sort.asc ? "ascending" : "descending") : "none"
                  }
                >
                  <button
                    type="button"
                    className={styles.modelTableSortButton}
                    onClick={() => handleSort(column.key)}
                  >
                    <span>{column.label}</span>
                    <span className={styles.modelTableArrow} aria-hidden="true">
                      {active ? (sort.asc ? "↑" : "↓") : "↕"}
                    </span>
                  </button>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {sortedModels.map((model) => {
            const metrics = metricsByModelId.get(model.id) ?? {};
            const href = `/entries/${entryId}/models/${model.id}`;
            return (
              <tr
                key={model.id}
                ref={(node) => {
                  if (node) {
                    rowRefs.current.set(model.id, node);
                  } else {
                    rowRefs.current.delete(model.id);
                  }
                }}
                className={styles.modelTableRow}
                onClick={(event) => {
                  // Let the name link handle modified clicks (open in new tab,
                  // keyboard activation) and clicks that land on the anchor.
                  if (
                    event.button !== 0 ||
                    event.metaKey ||
                    event.ctrlKey ||
                    event.shiftKey ||
                    (event.target as HTMLElement).closest("a")
                  ) {
                    return;
                  }
                  router.push(href);
                }}
              >
                <td className={styles.modelTableModelCell}>
                  <span className={styles.modelTableModelInner}>
                    <span
                      className={styles.modelTableThumb}
                      data-empty={model.thumbnail_image_url ? undefined : "true"}
                    >
                      {model.thumbnail_image_url ? (
                        // eslint-disable-next-line @next/next/no-img-element
                        <img
                          src={model.thumbnail_image_url}
                          alt=""
                          loading="lazy"
                        />
                      ) : (
                        <PlaceholderIcon />
                      )}
                    </span>
                    <span className={styles.modelTableNameBlock}>
                      <Link className={styles.modelTableName} href={href}>
                        {model.name}
                      </Link>
                      <span className={styles.modelTableDate}>
                        {formatDate(model.created_at)}
                      </span>
                    </span>
                  </span>
                </td>
                {metricColumns.map((column) => {
                  const value = metrics[column.key];
                  const hasValue = typeof value === "number";
                  const isBest =
                    hasValue && value === bestByColumn.get(column.key);
                  return (
                    <td
                      key={column.key}
                      className={styles.modelTableMetricCell}
                      data-active={sort?.key === column.key ? "true" : undefined}
                      data-status={hasValue ? column.status(value) : undefined}
                    >
                      {hasValue ? (
                        <span className={styles.modelTableMetricValue}>
                          {isBest ? (
                            <span
                              className={styles.modelTableBestDot}
                              aria-label="Best in column"
                            />
                          ) : null}
                          {numberFormatter.format(value)}
                        </span>
                      ) : (
                        <span className={styles.modelTableEmpty}>—</span>
                      )}
                    </td>
                  );
                })}
              </tr>
            );
          })}
        </tbody>
      </table>
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
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.4" fill="currentColor" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(120 12 12)" />
    </svg>
  );
}
