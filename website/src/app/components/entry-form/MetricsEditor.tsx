"use client";

import type { MetricKey } from "@/lib/api/entries";

import { FieldMessages } from "./FormFields";
import { PlusIcon } from "./icons";
import { inlineMessages, type FieldMessage } from "./messages";
import { METRIC_FIELDS } from "./types";
import type { MetricDraft, ProgramDraft } from "./types";
import { linkedRunId, sanitizeNumeric } from "./helpers";
import type { MetricSoftwareRule } from "./validation";
import styles from "./form.module.css";

/** The "Computed by" choice that keeps the scores' own software fields. */
const NAMED_SOFTWARE = "";

export default function MetricsEditor({
  metrics,
  messages,
  setMetrics,
  locked = {},
  programs,
  softwareRequired,
}: {
  metrics: MetricDraft[];
  /** Messages about a score are keyed by its metric key. */
  messages: readonly FieldMessage[];
  setMetrics: (next: MetricDraft[]) => void;
  /** Scores the PDB archive keeps up to date, each with its source badge.
   *  They are shown read-only. */
  locked?: Partial<Record<MetricKey, string>>;
  /** The model's programs, offered as what computed the scores. Given only
   *  where the programs are edited in the same form. */
  programs?: ProgramDraft[];
  /** Marks the software as required where the API requires it. */
  softwareRequired: MetricSoftwareRule;
}) {
  const programIds = new Set((programs ?? []).map((program) => program.id));
  const add = () =>
    setMetrics([...metrics, { id: crypto.randomUUID(), values: {} }]);
  const patch = (id: string, next: Partial<MetricDraft>) =>
    setMetrics(metrics.map((m) => (m.id === id ? { ...m, ...next } : m)));
  const remove = (id: string) =>
    setMetrics(metrics.filter((m) => m.id !== id));
  const holdsLockedScore = (metric: MetricDraft) =>
    METRIC_FIELDS.some((field) => locked[field.key] && metric.values[field.key]);
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

      {metrics.map((metric) => {
        const runId = linkedRunId(metric, programIds);
        return (
          <div key={metric.id} className={styles.metricCard}>
            <div className={styles.metricTop}>
              <span className={styles.metricTag}>Metrics · L3</span>
              {holdsLockedScore(metric) ? null : (
                <button
                  type="button"
                  className={styles.remove}
                  onClick={() => remove(metric.id)}
                  aria-label="Remove metrics"
                >
                  ×
                </button>
              )}
            </div>
            <div className={styles.metricGrid}>
              {programs ? (
                <label className={styles.metricField}>
                  <span>Computed by</span>
                  {/* Never preset from the programs: the program that built
                      the model is often not the one that scored it. */}
                  <select
                    className={styles.select}
                    value={runId ?? NAMED_SOFTWARE}
                    onChange={(event) =>
                      patch(metric.id, {
                        runId:
                          event.target.value === NAMED_SOFTWARE
                            ? undefined
                            : event.target.value,
                      })
                    }
                  >
                    <option value={NAMED_SOFTWARE}>Software named here</option>
                    {programs.map((program, index) => (
                      <option key={program.id} value={program.id}>
                        {programStepLabel(program, index)}
                      </option>
                    ))}
                  </select>
                  {runId !== null ? <FieldMessages messages={softwareMessages} /> : null}
                </label>
              ) : null}
              {runId !== null ? null : (
                <>
                  <label className={styles.metricField}>
                    <span>
                      {softwareRequired(metric) ? "Software (required)" : "Software"}
                    </span>
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
                </>
              )}
              {METRIC_FIELDS.map((field) => (
                <label key={field.key} className={styles.metricField}>
                  <span>
                    {field.label}
                    {locked[field.key] ? (
                      <span className={styles.sourceBadge}>{locked[field.key]}</span>
                    ) : null}
                  </span>
                  <input
                    className={styles.input}
                    type="text"
                    inputMode="decimal"
                    readOnly={locked[field.key] !== undefined}
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
        );
      })}
    </div>
  );
}

/** "Step 2: phenix.refine 1.21.2", as the programs section numbers them. */
function programStepLabel(program: ProgramDraft, index: number): string {
  const name = [program.name.trim(), program.version.trim()].filter(Boolean).join(" ");
  return `Step ${index + 1}: ${name || "unnamed program"}`;
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
