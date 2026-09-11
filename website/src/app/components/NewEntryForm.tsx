"use client";

import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";

import type { CreateEntryInput } from "@/lib/api/entries";
import { uploadFileToObjectStorage } from "@/lib/api/uploads";
import { createEntryAction } from "@/app/entries/new/actions";

import FilesEditor, { ProgressBar } from "./entry-form/FilesEditor";
import { UploadIcon } from "./entry-form/icons";
import {
  DRAFT_VERSION,
  METHODS,
  emptyEntryMetadataDraft,
} from "./entry-form/types";
import type {
  EntryMetadataDraft,
  ParsedFile,
  UploadStatus,
} from "./entry-form/types";
import {
  assignCanonicalArtifactTypes,
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

export default function NewEntryForm() {
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
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Draft persistence: restore prompt + gate so we never overwrite a saved
  // draft before the user decides whether to continue or discard it.
  const [draftPrompt, setDraftPrompt] = useState<StoredDraft | null>(null);
  const [storageReady, setStorageReady] = useState(false);

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
      title,
      thumbUrl,
      thumbPreview: httpOnly(thumbPreview),
      thumbUploadStatus: thumbUrl ? "uploaded" : "idle",
      files: files.filter(isPersistable).map(fileToDraft),
    };
    if (draftHasContent(draft)) {
      writeStoredDraft(draft);
    } else {
      clearStoredDraft();
    }
  }, [
    storageReady,
    entryId,
    title,
    thumbUrl,
    thumbPreview,
    files,
  ]);

  const continueDraft = () => {
    const draft = draftPrompt;
    if (!draft) {
      return;
    }
    setEntryId(draft.entryId);
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
    setDraftPrompt(null);
    setStorageReady(true);
  };

  const discardDraft = () => {
    clearStoredDraft();
    setDraftPrompt(null);
    setStorageReady(true);
    setTitle("");
  };

  const resetForm = () => {
    clearStoredDraft();
    setEntryId(crypto.randomUUID());
    setTitle("");
    activeThumbFileId.current = null;
    setThumbFile(null);
    setThumbPreview(null);
    setThumbUrl(null);
    setThumbProgress(0);
    setThumbUploadStatus("idle");
    setThumbUploadError(null);
    setFiles([]);
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
    files.some((file) => file.uploadStatus === "uploading");

  const hasFailedUploads =
    thumbUploadStatus === "failed" ||
    files.some((file) => file.uploadStatus === "failed");

  const canSubmit =
    title.trim().length > 0 &&
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
      if (!uploadsReady(files, thumbFile, thumbUrl)) {
        setError("Wait until all files are uploaded.");
        setSubmitting(false);
        return;
      }

      const pdb = metadata.pdb.trim();
      const input: CreateEntryInput = {
        external_refs: pdb ? { pdb: pdb.toUpperCase() } : undefined,
        resolution: metadata.resolution.trim()
          ? Number.parseFloat(metadata.resolution)
          : undefined,
        method: metadata.method.trim() || undefined,
        space_group: metadata.spaceGroup.trim() || undefined,
        title: title.trim(),
        thumbnail_image_url: thumbUrl,
        entities: files.map(toEntity),
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
      </section>

      {/* Properties of the structure itself, typed by the depositor. */}
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
          onAdd={addEntryFiles}
          onAddUrl={addEntryUrl}
          onRemove={(id) => setFiles((p) => p.filter((f) => f.id !== id))}
          onLevel={(id, level) =>
            setFiles((p) => p.map((f) => (f.id === id ? { ...f, level } : f)))
          }
          onPatch={(id, patch) =>
            setFiles((p) => p.map((f) => (f.id === id ? { ...f, ...patch } : f)))
          }
        />
      </section>

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
  const fileCount = draft.files.length;
  const parts: string[] = [];
  if (draft.title.trim()) {
    parts.push(`“${draft.title.trim()}”`);
  }
  if (fileCount > 0) {
    parts.push(`${fileCount} uploaded file${fileCount === 1 ? "" : "s"}`);
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
