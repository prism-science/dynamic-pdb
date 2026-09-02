"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";

import type { Entity, Entry } from "@/lib/api/entries";
import { formatEntryLabel } from "@/lib/entry-label";
import { getExtExperiment, type ExtExperiment } from "@/lib/api/ext";
import { createModelAction } from "@/app/entries/[entryId]/models/new/actions";

import ExtSourceField from "./entry-form/ExtSourceField";
import ModelDraftFields, {
  emptyModelDraft,
} from "./entry-form/ModelDraftFields";
import type { ModelDraft, ParsedFile } from "./entry-form/types";
import {
  buildCreateModelInput,
  formatSize,
  MODEL_FILE_MISSING,
  missingExpectedInputs,
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

  // The entry's own files, shaped like draft files so the pipeline editor can
  // offer them as inputs alongside the ones being uploaded here.
  const baselineFiles = useMemo(
    () => entryDataEntities.map(entityToBaselineFile),
    [entryDataEntities],
  );
  const baselineIds = useMemo(
    () => new Set(baselineFiles.map((file) => file.id)),
    [baselineFiles],
  );
  const [extExperimentId, setExtExperimentId] = useState<string | null>(null);
  const [extExperiment, setExtExperiment] = useState<ExtExperiment | null>(null);
  const [extExperimentError, setExtExperimentError] = useState<string | null>(
    null,
  );
  const [extExperimentLoading, setExtExperimentLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [missingWarning, setMissingWarning] = useState<
    { program: string; name: string; source: string }[]
  >([]);
  const [missingAcknowledged, setMissingAcknowledged] = useState(false);

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
    const missing = missingExpectedInputs(draft.programs);
    if (missing.length > 0 && !missingAcknowledged) {
      setMissingWarning(missing);
      setMissingAcknowledged(true);
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
        programInputEntityIds: draft.programs
          .flatMap((program) => program.inputFileIds)
          .filter((id) => baselineIds.has(id)),
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
            <strong>{formatEntryLabel(entry)}</strong>
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
        {validationMessage && validationMessage !== MODEL_FILE_MISSING ? (
          <p className={styles.inlineError}>{validationMessage}</p>
        ) : null}
        <ModelDraftFields
          draft={draft}
          entryId={entry.id}
          extExperiment={extExperiment}
          baselineFiles={baselineFiles}
          onUpdate={(update) => setDraft((current) => update(current))}
          onError={setError}
        />
      </section>

      {missingWarning.length > 0 ? (
        <div className={styles.missingWarning}>
          <strong>
            {missingWarning.length === 1
              ? "A declared input was not uploaded"
              : `${missingWarning.length} declared inputs were not uploaded`}
          </strong>
          <ul>
            {/* A chain missing twenty inputs is a list nobody reads; the count
                in the heading already carries the scale. */}
            {missingWarning.slice(0, 5).map((item) => (
              <li key={`${item.program}-${item.name}`}>
                <code>{item.name}</code> — input to {item.program}, declared in{" "}
                {item.source}
              </li>
            ))}
            {missingWarning.length > 5 ? (
              <li>and {missingWarning.length - 5} more</li>
            ) : null}
          </ul>
          <span>
            Upload them to complete the chain, or submit again to deposit
            without them.
          </span>
        </div>
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

// Entry-level artifacts are read-only here, so only the fields the pipeline
// editor and the graph actually render are filled in.
function entityToBaselineFile(entity: Entity): ParsedFile {
  const payload = (entity.payload ?? {}) as { type?: unknown; size?: unknown };
  return {
    id: entity.id,
    source: "url",
    name: entity.name,
    size: typeof payload.size === "number" ? payload.size : 0,
    type: typeof payload.type === "string" ? payload.type : "file",
    level: entity.level ?? "L0",
    authors: "",
    affiliation: "",
    url: "",
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
  };
}
