"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";

import type { Entity, Entry } from "@/lib/api/entries";
import { formatEntryLabel } from "@/lib/entry-label";
import { getExtExperiment, type ExtExperiment } from "@/lib/api/ext";
import { createModelAction } from "@/app/entries/[entryId]/models/new/actions";

import DraftRestorePrompt, {
  draftSummary,
} from "./entry-form/DraftRestorePrompt";
import ExtSourceField from "./entry-form/ExtSourceField";
import { SubmitBlockers } from "./entry-form/FormFields";
import ModelDraftFields, {
  emptyModelDraft,
} from "./entry-form/ModelDraftFields";
import {
  clearStoredModelDraft,
  modelDraftFromStored,
  modelDraftToStored,
  readStoredModelDraft,
  storedModelDraftHasContent,
  writeStoredModelDraft,
  type StoredModelDraft,
} from "./entry-form/drafts";
import { FIELDS, type SectionId } from "./entry-form/fields";
import { blockerLines, type FieldMessage } from "./entry-form/messages";
import type { ModelDraft, ParsedFile } from "./entry-form/types";
import { modelFormMessages } from "./entry-form/validation";
import {
  buildCreateModelInput,
  formatSize,
  missingExpectedInputs,
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
  // Errors the API returned for the last submit, shown beside their fields
  // until the depositor edits that section.
  const [apiMessages, setApiMessages] = useState<FieldMessage[]>([]);
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

  // Drafts are kept per entry, so a model half-described for one entry is
  // never offered on another. Nothing is saved until the depositor has
  // decided whether to continue a saved draft.
  const [draftPrompt, setDraftPrompt] = useState<StoredModelDraft | null>(null);
  const [storageReady, setStorageReady] = useState(false);

  useEffect(() => {
    const stored = readStoredModelDraft(entry.id);
    if (stored && storedModelDraftHasContent(stored)) {
      setDraftPrompt(stored);
    } else {
      if (stored) {
        clearStoredModelDraft(entry.id);
      }
      setStorageReady(true);
    }
  }, [entry.id]);

  const saveDraft = (current: ModelDraft) => {
    const stored = modelDraftToStored(current);
    if (storedModelDraftHasContent(stored)) {
      writeStoredModelDraft(entry.id, stored);
    } else {
      clearStoredModelDraft(entry.id);
    }
  };

  useEffect(() => {
    if (storageReady) {
      saveDraft(draft);
    }
    // saveDraft reads only entry.id besides the draft.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageReady, entry.id, draft]);

  const continueDraft = () => {
    if (!draftPrompt) {
      return;
    }
    setDraft(modelDraftFromStored(draftPrompt));
    setDraftPrompt(null);
    setStorageReady(true);
  };

  const discardDraft = () => {
    clearStoredModelDraft(entry.id);
    setDraftPrompt(null);
    setStorageReady(true);
  };

  const dropApiMessages = (section: SectionId) =>
    setApiMessages((current) =>
      current.filter((message) => FIELDS[message.field].section !== section),
    );

  const validationMessages = modelFormMessages(draft);
  const messages = [...validationMessages, ...apiMessages];
  const hasPendingUploads =
    draft.thumbUploadStatus === "uploading" ||
    draft.files.some((file) => file.uploadStatus === "uploading");
  const hasFailedUploads =
    draft.thumbUploadStatus === "failed" ||
    draft.files.some((file) => file.uploadStatus === "failed");
  const blockers = [
    ...blockerLines(validationMessages),
    ...(hasPendingUploads
      ? ["Uploads: wait until every file has finished uploading"]
      : []),
    ...(hasFailedUploads
      ? ["Uploads: remove the files that failed to upload, or add them again"]
      : []),
  ];
  const canSubmit = blockers.length === 0 && !submitting;

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
    setApiMessages([]);

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
      // The action redirects on success, so the draft is cleared up front,
      // and saved again if the action returns an error.
      clearStoredModelDraft(entry.id);
      const result = await createModelAction(entry.id, input);
      if (result) {
        saveDraft(draft);
        setError(result.error);
        setApiMessages(result.fieldErrors);
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
    <form className={styles.form} onSubmit={submit} noValidate>
      {draftPrompt ? (
        <DraftRestorePrompt
          summary={draftSummary(
            draftPrompt.title,
            draftPrompt.files.length,
            "an unfinished model",
          )}
          onContinue={continueDraft}
          onDiscard={discardDraft}
        />
      ) : null}

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

      <ModelDraftFields
        draft={draft}
        entryId={entry.id}
        extExperiment={extExperiment}
        baselineFiles={baselineFiles}
        messages={messages}
        onUpdate={(update) => setDraft((current) => update(current))}
        onEdit={dropApiMessages}
        onError={setError}
      />

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
      {submitting ? null : (
        <SubmitBlockers action="add the model" lines={blockers} />
      )}

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
    artifactType: "other",
    level: entity.level ?? "L0",
    authors: "",
    url: "",
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
  };
}
