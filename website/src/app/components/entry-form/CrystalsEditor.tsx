"use client";

import { useId } from "react";

import { crystalIds, emptyCrystal, emptyDiffraction } from "./crystals";
import { Field, FieldMessages } from "./FormFields";
import { sanitizeNumeric } from "./helpers";
import { PlusIcon } from "./icons";
import { inlineMessages, type FieldMessage } from "./messages";
import type { CrystalDraft } from "./types";
import styles from "./form.module.css";

/**
 * Crystals one card each, with their growth conditions and the temperature
 * of every dataset collected from them. The schema keeps crystals apart, so
 * there is no single "temperature" for an entry.
 */
export default function CrystalsEditor({
  crystals,
  onChange,
  messages,
}: {
  crystals: CrystalDraft[];
  onChange: (next: CrystalDraft[]) => void;
  messages: readonly FieldMessage[];
}) {
  const ids = crystalIds(crystals);

  const patch = (key: string, next: Partial<CrystalDraft>) =>
    onChange(
      crystals.map((crystal) =>
        crystal.key === key ? { ...crystal, ...next } : crystal,
      ),
    );

  return (
    <div className={styles.filesEditor}>
      {crystals.map((crystal, index) => (
        <CrystalCard
          key={crystal.key}
          crystal={crystal}
          crystalId={ids[index].crystalId}
          diffractionIds={ids[index].diffractionIds}
          messages={messages}
          onPatch={(next) => patch(crystal.key, next)}
          onRemove={() =>
            onChange(crystals.filter((item) => item.key !== crystal.key))
          }
        />
      ))}
      <button
        type="button"
        className={styles.fileDrop}
        onClick={() => onChange([...crystals, emptyCrystal()])}
      >
        <PlusIcon />
        Add crystal
      </button>
    </div>
  );
}

function CrystalCard({
  crystal,
  crystalId,
  diffractionIds,
  messages,
  onPatch,
  onRemove,
}: {
  crystal: CrystalDraft;
  crystalId: string;
  diffractionIds: string[];
  messages: readonly FieldMessage[];
  onPatch: (next: Partial<CrystalDraft>) => void;
  onRemove: () => void;
}) {
  const fieldId = useId();

  return (
    <div className={styles.metricCard}>
      <div className={styles.metricTop}>
        <span className={styles.programTag}>Crystal {crystalId}</span>
        <button
          type="button"
          className={styles.remove}
          onClick={onRemove}
          aria-label={`Remove crystal ${crystalId}`}
        >
          ×
        </button>
      </div>
      <FieldMessages
        messages={inlineMessages(messages, "crystal.block", crystalId)}
      />

      <div className={styles.fieldGrid}>
        <Field
          id="crystal.ph"
          htmlFor={`${fieldId}-ph`}
          messages={inlineMessages(messages, "crystal.ph", crystalId)}
        >
          <input
            id={`${fieldId}-ph`}
            className={styles.input}
            inputMode="decimal"
            value={crystal.ph}
            onChange={(event) =>
              onPatch({ ph: sanitizeNumeric(event.target.value) })
            }
            placeholder="e.g. 7.5"
            autoComplete="off"
          />
        </Field>
        <Field
          id="crystal.growthTemperature"
          htmlFor={`${fieldId}-growth`}
          messages={inlineMessages(messages, "crystal.growthTemperature", crystalId)}
        >
          <input
            id={`${fieldId}-growth`}
            className={styles.input}
            inputMode="decimal"
            value={crystal.growthTemperature}
            onChange={(event) =>
              onPatch({ growthTemperature: sanitizeNumeric(event.target.value) })
            }
            placeholder="e.g. 293"
            autoComplete="off"
          />
        </Field>
      </div>

      <Field id="diffraction.temperature">
        <div className={styles.datasetList}>
          {crystal.diffractions.map((diffraction, position) => {
            const datasetId = diffractionIds[position];
            const item = `${crystalId}/${datasetId}`;
            return (
              <div key={diffraction.key} className={styles.datasetRow}>
                <label
                  className={styles.datasetLabel}
                  htmlFor={`${fieldId}-dataset-${diffraction.key}`}
                >
                  Dataset {datasetId}
                </label>
                <input
                  id={`${fieldId}-dataset-${diffraction.key}`}
                  className={styles.input}
                  inputMode="decimal"
                  value={diffraction.temperature}
                  onChange={(event) =>
                    onPatch({
                      diffractions: crystal.diffractions.map((other) =>
                        other.key === diffraction.key
                          ? {
                              ...other,
                              temperature: sanitizeNumeric(event.target.value),
                            }
                          : other,
                      ),
                    })
                  }
                  placeholder="e.g. 100"
                  autoComplete="off"
                />
                <button
                  type="button"
                  className={styles.remove}
                  onClick={() =>
                    onPatch({
                      diffractions: crystal.diffractions.filter(
                        (other) => other.key !== diffraction.key,
                      ),
                    })
                  }
                  aria-label={`Remove dataset ${datasetId}`}
                >
                  ×
                </button>
                <div className={styles.datasetMessages}>
                  <FieldMessages
                    messages={inlineMessages(
                      messages,
                      "diffraction.temperature",
                      item,
                    )}
                  />
                </div>
              </div>
            );
          })}
          <button
            type="button"
            className={styles.chipAction}
            onClick={() =>
              onPatch({
                diffractions: [...crystal.diffractions, emptyDiffraction()],
              })
            }
          >
            + Add dataset
          </button>
        </div>
      </Field>
    </div>
  );
}
