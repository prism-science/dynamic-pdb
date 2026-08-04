"use client";

import { PlusIcon } from "./icons";
import { METRIC_FIELDS } from "./types";
import type { MetricDraft } from "./types";
import { sanitizeNumeric } from "./helpers";
import styles from "./form.module.css";

export default function MetricsEditor({
  metrics,
  setMetrics,
}: {
  metrics: MetricDraft[];
  setMetrics: (next: MetricDraft[]) => void;
}) {
  const add = () =>
    setMetrics([...metrics, { id: crypto.randomUUID(), values: {} }]);
  const patch = (id: string, next: Partial<MetricDraft>) =>
    setMetrics(metrics.map((m) => (m.id === id ? { ...m, ...next } : m)));
  const remove = (id: string) =>
    setMetrics(metrics.filter((m) => m.id !== id));

  return (
    <div className={styles.filesEditor}>
      {/* One set per model, so the button goes away once it exists. A second
          set would not survive the round trip anyway: the model page merges
          every metrics entity into a single record before displaying it. */}
      {metrics.length === 0 ? (
        <button type="button" className={styles.fileDrop} onClick={add}>
          <PlusIcon />
          Add metrics
        </button>
      ) : null}

      {metrics.map((metric) => (
        <div key={metric.id} className={styles.metricCard}>
          <div className={styles.metricTop}>
            <span className={styles.metricTag}>Metrics · L3</span>
            <button
              type="button"
              className={styles.remove}
              onClick={() => remove(metric.id)}
              aria-label="Remove metrics"
            >
              ×
            </button>
          </div>
          <div className={styles.metricGrid}>
            {METRIC_FIELDS.map((field) => (
              <label key={field.key} className={styles.metricField}>
                <span>{field.label}</span>
                <input
                  className={styles.input}
                  type="text"
                  inputMode="decimal"
                  value={metric.values[field.key] ?? ""}
                  onChange={(event) =>
                    patch(metric.id, {
                      values: {
                        ...metric.values,
                        [field.key]: sanitizeNumeric(event.target.value),
                      },
                    })
                  }
                  placeholder={field.example}
                />
              </label>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
