"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";

import type { CreateEntryInput } from "@/lib/api/entries";
import {
  extFileKey,
  getExtExperiment,
  parseExtExperimentRef,
  type ExtExperiment,
  type ExtFile,
} from "@/lib/api/ext";
import { uploadFileToObjectStorage } from "@/lib/api/uploads";
import { createEntryAction } from "@/app/entries/new/actions";

import ExtSourceField, {
  EXT_LINK_INVALID,
} from "./entry-form/ExtSourceField";
import FilesEditor, { ProgressBar } from "./entry-form/FilesEditor";
import ModelDraftFields, {
  emptyModelDraft,
} from "./entry-form/ModelDraftFields";
import type { EntryFacts } from "./entry-form/autoDetect";
import { PlusIcon, UploadIcon } from "./entry-form/icons";
import {
  DRAFT_VERSION,
  METHODS,
  emptyEntryMetadataDraft,
} from "./entry-form/types";
import type {
  EntryMetadataDraft,
  ModelDraft,
  ParsedFile,
  UploadStatus,
} from "./entry-form/types";
import {
  assignCanonicalArtifactTypes,
  buildCreateModelInput,
  extFileKeys,
  extFileToParsed,
  MODEL_FILE_MISSING,
  missingExpectedInputs,
  modelValidationMessage,
  parseFile,
  parseUrlFile,
  sanitizeNumeric,
  toEntity,
  uploadErrorMessage,
  uploadParsedFileNow,
  uploadStatusText,
  uploadsReady,
} from "./entry-form/helpers";
import {
  clearStoredDraft,
  draftHasContent,
  fileFromDraft,
  fileToDraft,
  httpOnly,
  isPersistable,
  modelFromDraft,
  modelToDraft,
  readStoredDraft,
  writeStoredDraft,
  type StoredDraft,
} from "./entry-form/drafts";
import styles from "./entry-form/form.module.css";

// The form's building blocks live in ./entry-form so the add-model form can
// reuse them; re-exported here because callers and tests import them by name.
export * from "./entry-form/types";
export * from "./entry-form/helpers";
export * from "./entry-form/drafts";

type NewEntryFormProps = {
  extExperimentId?: string | null;
};

export default function NewEntryForm({
  extExperimentId: initialExtExperimentId = null,
}: NewEntryFormProps) {
  // The ?ext_experiment_id query param only seeds the field; from here on the
  // link is form state the user can change or clear.
  const seededExtExperimentId =
    parseExtExperimentRef(initialExtExperimentId ?? "") ?? null;
  const [extExperimentId, setExtExperimentId] = useState<string | null>(
    seededExtExperimentId,
  );
  const [extExperiment, setExtExperiment] = useState<ExtExperiment | null>(null);
  const [extExperimentError, setExtExperimentError] = useState<string | null>(
    initialExtExperimentId && !seededExtExperimentId ? EXT_LINK_INVALID : null,
  );
  const [extExperimentLoading, setExtExperimentLoading] = useState(
    Boolean(seededExtExperimentId),
  );
  const [entryId, setEntryId] = useState(() => crypto.randomUUID());
  const [title, setTitle] = useState("");
  const activeThumbFileId = useRef<string | null>(null);
  const [thumbFile, setThumbFile] = useState<File | null>(null);
  const [thumbPreview, setThumbPreview] = useState<string | null>(null);
  const [thumbUrl, setThumbUrl] = useState<string | null>(null);
  const [thumbProgress, setThumbProgress] = useState(0);
  const [thumbUploadStatus, setThumbUploadStatus] = useState<UploadStatus>("idle");
  const [thumbUploadError, setThumbUploadError] = useState<string | null>(null);
  const [metadata, setMetadata] = useState<EntryMetadataDraft>(
    emptyEntryMetadataDraft,
  );
  const [files, setFiles] = useState<ParsedFile[]>([]);
  const [models, setModels] = useState<ModelDraft[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [missingWarning, setMissingWarning] = useState<
    { program: string; name: string; source: string }[]
  >([]);
  const [missingAcknowledged, setMissingAcknowledged] = useState(false);

  // Draft persistence: restore prompt + gate so we never overwrite a saved
  // draft before the user decides whether to continue or discard it.
  const [draftPrompt, setDraftPrompt] = useState<StoredDraft | null>(null);
  const [storageReady, setStorageReady] = useState(false);

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
      .then((experiment) => {
        setExtExperiment(experiment);
        setTitle((current) => current || experiment.name);
      })
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

  const linkExtExperiment = (experimentId: string) => {
    setExtExperimentError(null);
    setExtExperimentId(experimentId);
  };

  const unlinkExtExperiment = () => {
    // Files already pulled from Ext keep working — each one carries its own
    // experiment reference — so only the form-level link is dropped.
    setExtExperimentId(null);
    setExtExperiment(null);
    setExtExperimentError(null);
  };

  useEffect(() => {
    const stored = readStoredDraft();
    if (stored && draftHasContent(stored)) {
      setDraftPrompt(stored);
    } else {
      if (stored) {
        clearStoredDraft();
      }
      setStorageReady(true);
    }
  }, []);

  useEffect(() => {
    if (!storageReady) {
      return;
    }
    const draft: StoredDraft = {
      version: DRAFT_VERSION,
      entryId,
      extExperimentId,
      title,
      thumbUrl,
      thumbPreview: httpOnly(thumbPreview),
      thumbUploadStatus: thumbUrl ? "uploaded" : "idle",
      files: files.filter(isPersistable).map(fileToDraft),
      models: models.map(modelToDraft),
    };
    if (draftHasContent(draft)) {
      writeStoredDraft(draft);
    } else {
      clearStoredDraft();
    }
  }, [
    storageReady,
    entryId,
    extExperimentId,
    title,
    thumbUrl,
    thumbPreview,
    files,
    models,
  ]);

  const continueDraft = () => {
    const draft = draftPrompt;
    if (!draft) {
      return;
    }
    setEntryId(draft.entryId);
    const draftExtExperimentId = parseExtExperimentRef(
      typeof draft.extExperimentId === "string" ? draft.extExperimentId : "",
    );
    if (draftExtExperimentId) {
      linkExtExperiment(draftExtExperimentId);
    }
    setTitle(draft.title);
    setThumbUrl(draft.thumbUrl);
    setThumbPreview(draft.thumbPreview ?? null);
    setThumbUploadStatus(draft.thumbUrl ? "uploaded" : "idle");
    setThumbProgress(draft.thumbUrl ? 1 : 0);
    setFiles(
      assignCanonicalArtifactTypes(
        [],
        draft.files.map(fileFromDraft),
        "entry",
      ),
    );
    setModels(draft.models.map(modelFromDraft));
    setDraftPrompt(null);
    setStorageReady(true);
  };

  const discardDraft = () => {
    clearStoredDraft();
    setDraftPrompt(null);
    setStorageReady(true);
    // "Start fresh" means a blank form: drop the linked experiment along with
    // the title it prefilled.
    unlinkExtExperiment();
    setTitle("");
  };

  const resetForm = () => {
    clearStoredDraft();
    setEntryId(crypto.randomUUID());
    // The link is something the user entered, so it resets with everything
    // else instead of re-seeding the title from the experiment.
    unlinkExtExperiment();
    setTitle("");
    activeThumbFileId.current = null;
    setThumbFile(null);
    setThumbPreview(null);
    setThumbUrl(null);
    setThumbProgress(0);
    setThumbUploadStatus("idle");
    setThumbUploadError(null);
    setFiles([]);
    setModels([]);
    setError(null);
  };

  const onThumb = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    const nextThumbFileId = crypto.randomUUID();
    activeThumbFileId.current = nextThumbFileId;
    setThumbFile(file);
    setThumbPreview(URL.createObjectURL(file));
    setThumbUrl(null);
    setThumbProgress(0);
    setThumbUploadStatus("uploading");
    setThumbUploadError(null);
    void uploadEntryThumbnail(file, nextThumbFileId);
  };

  const addEntryFiles = async (list: File[]) => {
    const parsed = await Promise.all(list.map((file) => parseFile(file, "L0")));
    setFiles((current) => [
      ...current,
      ...assignCanonicalArtifactTypes(current, parsed, "entry"),
    ]);
    parsed.forEach((file) => {
      void uploadParsedFileNow(file, { entryId, modelId: null }, patchEntryFile);
    });
  };

  const addEntryUrl = async (rawUrl: string) => {
    const parsed = await parseUrlFile(rawUrl, "L0");
    if (!parsed) {
      return;
    }
    setFiles((current) => [
      ...current,
      ...assignCanonicalArtifactTypes(current, [parsed], "entry"),
    ]);
  };

  const addEntryExtFiles = (selected: ExtFile[]) => {
    if (!extExperiment) {
      return;
    }
    const existing = extFileKeys(files);
    const parsed = selected
      .filter((file) => !existing.has(extFileKey(file)))
      .map((file) => extFileToParsed(file, extExperiment.id, "L0"));
    if (parsed.length === 0) {
      return;
    }
    setFiles((current) => [
      ...current,
      ...assignCanonicalArtifactTypes(current, parsed, "entry"),
    ]);
  };

  const addModel = () => {
    setModels((prev) => [...prev, emptyModelDraft()]);
  };

  const removeModel = (id: string) => {
    setModels((prev) => prev.filter((modelDraft) => modelDraft.id !== id));
  };

  const patchEntryFile = (id: string, patch: Partial<ParsedFile>) => {
    setFiles((prev) =>
      prev.map((file) => (file.id === id ? { ...file, ...patch } : file)),
    );
  };

  const uploadEntryThumbnail = async (file: File, fileId: string) => {
    try {
      const url = await uploadFileToObjectStorage(
        file,
        { entryId, modelId: null, entityId: fileId },
        (fraction) => {
          if (activeThumbFileId.current === fileId) {
            setThumbProgress(fraction);
          }
        },
      );
      if (activeThumbFileId.current !== fileId) {
        return;
      }
      setThumbUrl(url);
      setThumbProgress(1);
      setThumbUploadStatus("uploaded");
      setThumbUploadError(null);
    } catch (error) {
      if (activeThumbFileId.current !== fileId) {
        return;
      }
      setThumbUploadStatus("failed");
      setThumbUploadError(uploadErrorMessage(error));
    }
  };

  const hasPendingUploads =
    thumbUploadStatus === "uploading" ||
    files.some((file) => file.uploadStatus === "uploading") ||
    models.some(
      (modelDraft) =>
        modelDraft.thumbUploadStatus === "uploading" ||
        modelDraft.files.some((file) => file.uploadStatus === "uploading"),
    );

  const hasFailedUploads =
    thumbUploadStatus === "failed" ||
    files.some((file) => file.uploadStatus === "failed") ||
    models.some(
      (modelDraft) =>
        modelDraft.thumbUploadStatus === "failed" ||
        modelDraft.files.some((file) => file.uploadStatus === "failed"),
    );

  const canSubmit =
    title.trim().length > 0 &&
    models.every(
      (modelDraft) =>
        modelDraft.title.trim().length > 0 && !modelValidationMessage(modelDraft),
    ) &&
    !hasPendingUploads &&
    !hasFailedUploads &&
    !submitting;

  // Prefill only: the header supplies what the depositor has not typed, and
  // never argues with what they have.
  const applyEntryFacts = useCallback((facts: EntryFacts) => {
    setMetadata((current) => ({
      pdb: current.pdb || facts.pdb || "",
      resolution:
        current.resolution ||
        (facts.resolution !== undefined ? String(facts.resolution) : ""),
      method: current.method || facts.method || "",
      spaceGroup: current.spaceGroup || facts.spaceGroup || "",
    }));
  }, []);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    // Inputs a run named but nobody uploaded. Worth stopping over once — a
    // deposit missing its free-R set is easy to make and hard to notice later
    // — but never worth blocking: the record is still true without them.
    const missing = models.flatMap((modelDraft) =>
      missingExpectedInputs(modelDraft.programs),
    );
    if (missing.length > 0 && !missingAcknowledged) {
      setMissingWarning(missing);
      setMissingAcknowledged(true);
      return;
    }
    setSubmitting(true);
    setError(null);

    try {
      if (!uploadsReady(files, models, thumbFile, thumbUrl)) {
        setError("Wait until all files are uploaded.");
        setSubmitting(false);
        return;
      }

      const input: CreateEntryInput = {
        metadata: {
          pdb: metadata.pdb.trim() || null,
          resolution: metadata.resolution.trim()
            ? Number.parseFloat(metadata.resolution)
            : null,
          method: metadata.method.trim() || null,
          space_group: metadata.spaceGroup.trim() || null,
        },
        title: title.trim(),
        thumbnail_image_url: thumbUrl,
        entities: files.map(toEntity),
        models: models.map((modelDraft) => buildCreateModelInput(modelDraft)),
      };

      // The server action redirects on success, so clear the saved draft up
      // front; the persistence effect re-saves if the action returns an error.
      clearStoredDraft();
      const result = await createEntryAction(input);
      if (result?.error) {
        setError(result.error);
        setSubmitting(false);
      }
    } catch (error) {
      setError(
        error instanceof Error ? error.message : "Failed to upload files.",
      );
      setSubmitting(false);
    }
    // On success the action redirects to "/".
  };

  return (
    <form className={styles.form} onSubmit={submit}>
      {draftPrompt ? (
        <DraftRestorePrompt
          draft={draftPrompt}
          onContinue={continueDraft}
          onDiscard={discardDraft}
        />
      ) : null}

      {/* Title and preview image are one identity block. The
          thumbnail is placed first visually by CSS but stays last in the DOM so
          the keyboard lands on the title field first. */}
      <section className={styles.field}>
        <div className={styles.identityRow}>
          <div className={styles.identityMain}>
            <div className={styles.field}>
              <label className={styles.label} htmlFor="entry-title">
                Title
              </label>
              <input
                id="entry-title"
                className={styles.input}
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="e.g. Hen egg-white lysozyme"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
            </div>

          </div>

          <div className={styles.identityThumb}>
            <span className={styles.label}>Preview image</span>
            <label className={styles.thumbDrop}>
              {thumbPreview ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img className={styles.thumbImg} src={thumbPreview} alt="" />
              ) : (
                <span className={styles.thumbHint}>
                  <UploadIcon />
                  Choose an image
                </span>
              )}
              {thumbUploadStatus === "uploading" ? (
                <span className={styles.thumbOverlay}>
                  <ProgressBar value={thumbProgress} />
                </span>
              ) : null}
              <input
                type="file"
                accept="image/*"
                className={styles.hiddenInput}
                onChange={onThumb}
              />
            </label>
            {thumbUploadStatus === "failed" ? (
              <span className={styles.uploadStatus} data-state="failed">
                {uploadStatusText(thumbUploadStatus, thumbUploadError)}
              </span>
            ) : null}
          </div>
        </div>

        <ExtSourceField
          experiment={extExperiment}
          error={extExperimentError}
          loading={extExperimentLoading}
          onLink={linkExtExperiment}
          onUnlink={unlinkExtExperiment}
        />
      </section>

      {/* Properties of the structure itself. Prefilled from the model file's
          header when one is uploaded — the depositor confirms rather than
          retypes. */}
      <section className={styles.field}>
        <span className={styles.label}>Info</span>
        <div className={styles.entryMetaGrid}>
          <label className={styles.metricField}>
            <span>PDB ID</span>
            <input
              className={styles.input}
              value={metadata.pdb}
              onChange={(event) =>
                setMetadata((current) => ({
                  ...current,
                  pdb: event.target.value.toUpperCase(),
                }))
              }
              placeholder="e.g. 5GY3"
              maxLength={4}
              autoComplete="off"
              data-1p-ignore
              data-lpignore="true"
              data-form-type="other"
            />
          </label>
          <label className={styles.metricField}>
            <span>Resolution, Å</span>
            <input
              className={styles.input}
              inputMode="decimal"
              value={metadata.resolution}
              onChange={(event) =>
                setMetadata((current) => ({
                  ...current,
                  resolution: sanitizeNumeric(event.target.value),
                }))
              }
              placeholder="e.g. 1.77"
              autoComplete="off"
            />
          </label>
          <label className={styles.metricField}>
            <span>Method</span>
            <select
              className={styles.select}
              value={metadata.method}
              onChange={(event) =>
                setMetadata((current) => ({
                  ...current,
                  method: event.target.value,
                }))
              }
            >
              <option value="">—</option>
              {METHODS.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
          <label className={styles.metricField}>
            <span>Space group</span>
            <input
              className={styles.input}
              value={metadata.spaceGroup}
              onChange={(event) =>
                setMetadata((current) => ({
                  ...current,
                  spaceGroup: event.target.value,
                }))
              }
              placeholder="e.g. P 21 21 21"
              autoComplete="off"
            />
          </label>
        </div>
      </section>

      <section className={styles.field}>
        <span className={styles.label}>Baseline data</span>
        <FilesEditor
          files={files}
          lockLevel
          extExperiment={extExperiment}
          onAdd={addEntryFiles}
          onAddUrl={addEntryUrl}
          onAddExt={addEntryExtFiles}
          onRemove={(id) => setFiles((p) => p.filter((f) => f.id !== id))}
          onLevel={(id, level) =>
            setFiles((p) => p.map((f) => (f.id === id ? { ...f, level } : f)))
          }
          onPatch={(id, patch) =>
            setFiles((p) => p.map((f) => (f.id === id ? { ...f, ...patch } : f)))
          }
        />
      </section>

      <section className={styles.field}>
        <div className={styles.modelDraftHead}>
          <span className={styles.label}>Models</span>
          <button type="button" className={styles.addModel} onClick={addModel}>
            <PlusIcon />
            Add model
          </button>
        </div>

        {models.length === 0 ? (
          <p className={styles.modelDraftEmpty}>No models yet.</p>
        ) : (
          <div className={styles.modelDraftList}>
            {models.map((modelDraft, index) => (
              <div key={modelDraft.id} className={styles.modelDraftCard}>
                {(() => {
                  const validationMessage = modelValidationMessage(modelDraft);
                  return validationMessage &&
                    validationMessage !== MODEL_FILE_MISSING ? (
                    <p className={styles.inlineError}>{validationMessage}</p>
                  ) : null;
                })()}
                <div className={styles.modelDraftCardHead}>
                  <span className={styles.modelDraftIndex}>
                    Model {index + 1}
                  </span>
                  <button
                    type="button"
                    className={styles.remove}
                    onClick={() => removeModel(modelDraft.id)}
                    aria-label="Remove model"
                  >
                    ×
                  </button>
                </div>
                <ModelDraftFields
                  draft={modelDraft}
                  entryId={entryId}
                  extExperiment={extExperiment}
                  baselineFiles={files}
                  onEntryFacts={applyEntryFacts}
                  onUpdate={(update) =>
                    setModels((prev) =>
                      prev.map((current) =>
                        current.id === modelDraft.id ? update(current) : current,
                      ),
                    )
                  }
                  onError={setError}
                />
              </div>
            ))}
          </div>
        )}
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
        <button type="button" className={styles.resetBtn} onClick={resetForm}>
          Reset form
        </button>
        <div className={styles.actionsRight}>
          <a className={styles.secondary} href="/browse">
            Cancel
          </a>
          <button type="submit" className={styles.primary} disabled={!canSubmit}>
            {submitting
              ? "Creating…"
              : hasPendingUploads
                ? "Uploading…"
                : "Create entry"}
          </button>
        </div>
      </div>
    </form>
  );
}

function DraftRestorePrompt({
  draft,
  onContinue,
  onDiscard,
}: {
  draft: StoredDraft;
  onContinue: () => void;
  onDiscard: () => void;
}) {
  const fileCount =
    draft.files.length +
    draft.models.reduce((sum, modelDraft) => sum + modelDraft.files.length, 0);
  const parts: string[] = [];
  if (draft.title.trim()) {
    parts.push(`“${draft.title.trim()}”`);
  }
  if (fileCount > 0) {
    parts.push(`${fileCount} uploaded file${fileCount === 1 ? "" : "s"}`);
  }
  if (draft.models.length > 0) {
    parts.push(
      `${draft.models.length} model${draft.models.length === 1 ? "" : "s"}`,
    );
  }
  if (draft.extExperimentId) {
    parts.push("a linked Ext experiment");
  }
  const summary = parts.length > 0 ? parts.join(" · ") : "an unfinished entry";

  return (
    <div className={styles.modalBackdrop} role="dialog" aria-modal="true">
      <div className={styles.modalCard}>
        <h2 className={styles.modalTitle}>Continue where you left off?</h2>
        <p className={styles.modalText}>
          You have a saved draft: {summary}. Continue editing it, or start over
          with a blank form.
        </p>
        <div className={styles.modalActions}>
          <button
            type="button"
            className={styles.secondary}
            onClick={onDiscard}
          >
            Start fresh
          </button>
          <button
            type="button"
            className={styles.primary}
            onClick={onContinue}
          >
            Continue
          </button>
        </div>
      </div>
    </div>
  );
}
