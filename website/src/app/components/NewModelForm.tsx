"use client";

import { useEffect, useState, type FormEvent } from "react";

import type { Entity, Entry } from "@/lib/api/entries";
import { getExtExperiment, type ExtExperiment } from "@/lib/api/ext";
import { createModelAction } from "@/app/entries/[entryId]/models/new/actions";

import ExtSourceField from "./entry-form/ExtSourceField";
import ModelDraftFields, {
  emptyModelDraft,
} from "./entry-form/ModelDraftFields";
import type { ModelDraft } from "./entry-form/types";
import {
  buildCreateModelInput,
  formatSize,
  modelValidationMessage,
} from "./entry-form/helpers";
import styles from "./entry-form/form.module.css";

export default function NewModelForm({
  entry,
  entryDataEntities,
  modelCount,
}: {
  entry: Entry;
  entryDataEntities: Entity[];
  modelCount: number;
}) {
  const [draft, setDraft] = useState<ModelDraft>(emptyModelDraft);
  const [programInputs, setProgramInputs] = useState<Set<string>>(
    () => new Set(),
  );
  const [extExperimentId, setExtExperimentId] = useState<string | null>(null);
  const [extExperiment, setExtExperiment] = useState<ExtExperiment | null>(null);
  const [extExperimentError, setExtExperimentError] = useState<string | null>(
    null,
  );
  const [extExperimentLoading, setExtExperimentLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setExtExperiment(null);
    if (!extExperimentId) {
      setExtExperimentLoading(false);
      return;
    }
    setExtExperimentError(null);
    setExtExperimentLoading(true);

    const controller = new AbortController();
    void getExtExperiment(extExperimentId, controller.signal)
      .then(setExtExperiment)
      .catch((loadError: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setExtExperimentId(null);
        setExtExperimentError(
          loadError instanceof Error
            ? loadError.message
            : "Could not load the Ext experiment.",
        );
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setExtExperimentLoading(false);
        }
      });
    return () => controller.abort();
  }, [extExperimentId]);

  useEffect(() => {
    if (!draft.program) {
      setProgramInputs((current) => (current.size === 0 ? current : new Set()));
    }
  }, [draft.program]);

  const toggleProgramInput = (entityId: string) =>
    setProgramInputs((current) => {
      const next = new Set(current);
      if (next.has(entityId)) {
        next.delete(entityId);
      } else {
        next.add(entityId);
      }
      return next;
    });

  const validationMessage = modelValidationMessage(draft);
  const hasPendingUploads =
    draft.thumbUploadStatus === "uploading" ||
    draft.files.some((file) => file.uploadStatus === "uploading");
  const hasFailedUploads =
    draft.thumbUploadStatus === "failed" ||
    draft.files.some((file) => file.uploadStatus === "failed");
  const canSubmit =
    draft.name.trim().length > 0 &&
    !validationMessage &&
    !hasPendingUploads &&
    !hasFailedUploads &&
    !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);

    try {
      if (draft.thumbFile && !draft.thumbUrl) {
        setError("Wait until all files are uploaded.");
        setSubmitting(false);
        return;
      }
      if (
        draft.files.some(
          (file) => file.uploadStatus !== "uploaded" || !file.url,
        )
      ) {
        setError("Wait until all files are uploaded.");
        setSubmitting(false);
        return;
      }

      const input = buildCreateModelInput(draft, {
        programInputEntityIds: draft.program
          ? Array.from(programInputs)
          : undefined,
      });
      const result = await createModelAction(entry.id, input);
      if (result?.error) {
        setError(result.error);
        setSubmitting(false);
      }
    } catch (submitError) {
      setError(
        submitError instanceof Error
          ? submitError.message
          : "Failed to add the model.",
      );
      setSubmitting(false);
    }
    // On success the action redirects back to the entry.
  };

  return (
    <form className={styles.form} onSubmit={submit}>
      <section className={styles.field}>
        <span className={styles.label}>Adding to</span>
        <div className={styles.sourceRow}>
          <span className={styles.entryMark}>
            {entry.thumbnail_image_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={entry.thumbnail_image_url} alt="" />
            ) : null}
          </span>
          <span className={styles.sourceMeta}>
            <strong>{entry.name}</strong>
            <span>{entry.id}</span>
          </span>
          <span className={styles.sourceCount}>
            {`${modelCount} ${modelCount === 1 ? "model" : "models"} · ${entryDataEntities.length} ${
              entryDataEntities.length === 1 ? "file" : "files"
            }`}
          </span>
        </div>
        <ExtSourceField
          experiment={extExperiment}
          error={extExperimentError}
          loading={extExperimentLoading}
          onLink={(experimentId) => {
            setExtExperimentError(null);
            setExtExperimentId(experimentId);
          }}
          onUnlink={() => {
            setExtExperimentId(null);
            setExtExperiment(null);
            setExtExperimentError(null);
          }}
        />
      </section>

      <section className={styles.field}>
        <span className={styles.label}>Model</span>
        {validationMessage ? (
          <p className={styles.inlineError}>{validationMessage}</p>
        ) : null}
        <ModelDraftFields
          draft={draft}
          entryId={entry.id}
          extExperiment={extExperiment}
          onUpdate={(update) => setDraft((current) => update(current))}
          onError={setError}
        />
      </section>

      {draft.program && entryDataEntities.length > 0 ? (
        <section className={styles.field}>
          <span className={styles.label}>Program inputs from this entry</span>
          <ul className={styles.inputPicker}>
            {entryDataEntities.map((entity) => (
              <li key={entity.id}>
                <label>
                  <input
                    type="checkbox"
                    checked={programInputs.has(entity.id)}
                    onChange={() => toggleProgramInput(entity.id)}
                  />
                  {entity.level ? (
                    <span className={styles.levelBadge} data-level={entity.level}>
                      {entity.level}
                    </span>
                  ) : null}
                  <span className={styles.inputPickerName}>{entity.name}</span>
                  <span className={styles.inputPickerMeta}>
                    {entityMeta(entity)}
                  </span>
                </label>
              </li>
            ))}
          </ul>
          <span className={styles.sourceHint}>
            Recorded as inputs to the program that produced this model.
          </span>
        </section>
      ) : null}

      {error ? <p className={styles.error}>{error}</p> : null}

      <div className={styles.actions}>
        <div className={styles.actionsRight}>
          <a
            className={styles.secondary}
            href={`/entries/${encodeURIComponent(entry.id)}`}
          >
            Cancel
          </a>
          <button type="submit" className={styles.primary} disabled={!canSubmit}>
            {submitting
              ? "Adding…"
              : hasPendingUploads
                ? "Uploading…"
                : "Add model"}
          </button>
        </div>
      </div>
    </form>
  );
}

function entityMeta(entity: Entity): string {
  const payload = entity.payload as { type?: string; size?: number };
  return [
    payload.type?.toUpperCase(),
    typeof payload.size === "number" && payload.size > 0
      ? formatSize(payload.size)
      : null,
  ]
    .filter(Boolean)
    .join(" · ");
}
