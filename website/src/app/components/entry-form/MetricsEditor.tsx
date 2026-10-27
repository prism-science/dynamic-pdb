"use client";

import { FieldMessages } from "./FormFields";
import { PlusIcon } from "./icons";
import { inlineMessages, type FieldMessage } from "./messages";
import { METRIC_FIELDS } from "./types";
import type { MetricDraft } from "./types";
import { sanitizeNumeric } from "./helpers";
import styles from "./form.module.css";

export default function MetricsEditor({
  metrics,
  messages,
  setMetrics,
}: {
  metrics: MetricDraft[];
  /** Messages about a score are keyed by its metric key. */
  messages: readonly FieldMessage[];
  setMetrics: (next: MetricDraft[]) => void;
}) {
  const add = () =>
    setMetrics([...metrics, { id: crypto.randomUUID(), values: {} }]);
  const patch = (id: string, next: Partial<MetricDraft>) =>
    setMetrics(metrics.map((m) => (m.id === id ? { ...m, ...next } : m)));
  const remove = (id: string) =>
    setMetrics(metrics.filter((m) => m.id !== id));
  // The API reports software errors per score; the set has one software field.
  const softwareMessages = uniqueByText(
    messages.filter(
      (message) => message.field === "model.metrics.software" && !message.missing,
    ),
  );

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
            <label className={styles.metricField}>
              <span>Software</span>
              <input
                className={styles.input}
                type="text"
                value={metric.softwareName ?? ""}
                onChange={(event) =>
                  patch(metric.id, { softwareName: event.target.value })
                }
                placeholder="e.g. MolProbity"
              />
              <FieldMessages messages={softwareMessages} />
            </label>
            <label className={styles.metricField}>
              <span>Software version</span>
              <input
                className={styles.input}
                type="text"
                value={metric.softwareVersion ?? ""}
                onChange={(event) =>
                  patch(metric.id, { softwareVersion: event.target.value })
                }
                placeholder="e.g. 4.5.2"
              />
            </label>
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
                <FieldMessages
                  messages={inlineMessages(messages, "model.metrics", field.key)}
                />
              </label>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function uniqueByText(messages: readonly FieldMessage[]): FieldMessage[] {
  const seen = new Set<string>();
  return messages.filter((message) => {
    if (seen.has(message.message)) {
      return false;
    }
    seen.add(message.message);
    return true;
  });
}
