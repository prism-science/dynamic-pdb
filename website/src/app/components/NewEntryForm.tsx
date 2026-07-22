"use client";

import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
  type KeyboardEvent,
} from "react";

import type {
  CreateEntryInput,
  EntityLevel,
  FastaMetadata,
} from "@/lib/api/entries";
import {
  uploadFileToObjectStorage,
  type FileUploadContext,
} from "@/lib/api/uploads";
import { createEntryAction } from "@/app/entries/new/actions";
import SequenceView from "./SequenceView";

import styles from "./NewEntryForm.module.css";

type UploadStatus = "idle" | "uploading" | "uploaded" | "failed";

type FileSource = "upload" | "url";

type ParsedFile = {
  id: string;
  file?: File;
  source: FileSource;
  name: string;
  size: number;
  type: string;
  level: EntityLevel;
  authors: string;
  affiliation: string;
  metadata?: Record<string, unknown>;
  preview?: string;
  url: string;
  progress: number;
  uploadStatus: UploadStatus;
  uploadError: string | null;
};

type MetricDraft = {
  id: string;
  values: Record<string, string>;
};

type ExperimentDraft = {
  id: string;
  name: string;
  description: string;
  thumbFileId: string;
  thumbFile: File | null;
  thumbPreview: string | null;
  thumbUrl: string | null;
  thumbProgress: number;
  thumbUploadStatus: UploadStatus;
  thumbUploadError: string | null;
  files: ParsedFile[];
  metrics: MetricDraft[];
};

const LEVELS: EntityLevel[] = ["L0", "L1", "L2", "L3"];

const METRIC_FIELDS: { key: string; label: string; example: string }[] = [
  { key: "r_work", label: "R-work", example: "e.g. 0.196" },
  { key: "r_free", label: "R-free", example: "e.g. 0.231" },
  { key: "rscc", label: "RSCC", example: "e.g. 0.96" },
  { key: "cc", label: "CC", example: "e.g. 0.98" },
];

const DRAFT_STORAGE_KEY = "dpdb:new-entry-draft";
const DRAFT_VERSION = 1;

export default function NewEntryForm() {
  const [entryId, setEntryId] = useState(() => crypto.randomUUID());
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const activeThumbFileId = useRef<string | null>(null);
  const [thumbFile, setThumbFile] = useState<File | null>(null);
  const [thumbPreview, setThumbPreview] = useState<string | null>(null);
  const [thumbUrl, setThumbUrl] = useState<string | null>(null);
  const [thumbProgress, setThumbProgress] = useState(0);
  const [thumbUploadStatus, setThumbUploadStatus] = useState<UploadStatus>("idle");
  const [thumbUploadError, setThumbUploadError] = useState<string | null>(null);
  const [files, setFiles] = useState<ParsedFile[]>([]);
  const [experiments, setExperiments] = useState<ExperimentDraft[]>([]);
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
      name,
      description,
      thumbUrl,
      thumbPreview: httpOnly(thumbPreview),
      thumbUploadStatus: thumbUrl ? "uploaded" : "idle",
      files: files.filter(isPersistable).map(fileToDraft),
      experiments: experiments.map(experimentToDraft),
    };
    if (draftHasContent(draft)) {
      writeStoredDraft(draft);
    } else {
      clearStoredDraft();
    }
  }, [storageReady, entryId, name, description, thumbUrl, thumbPreview, files, experiments]);

  const continueDraft = () => {
    const draft = draftPrompt;
    if (!draft) {
      return;
    }
    setEntryId(draft.entryId);
    setName(draft.name);
    setDescription(draft.description);
    setThumbUrl(draft.thumbUrl);
    setThumbPreview(draft.thumbPreview ?? null);
    setThumbUploadStatus(draft.thumbUrl ? "uploaded" : "idle");
    setThumbProgress(draft.thumbUrl ? 1 : 0);
    setFiles(draft.files.map(fileFromDraft));
    setExperiments(draft.experiments.map(experimentFromDraft));
    setDraftPrompt(null);
    setStorageReady(true);
  };

  const discardDraft = () => {
    clearStoredDraft();
    setDraftPrompt(null);
    setStorageReady(true);
  };

  const resetForm = () => {
    clearStoredDraft();
    setEntryId(crypto.randomUUID());
    setName("");
    setDescription("");
    activeThumbFileId.current = null;
    setThumbFile(null);
    setThumbPreview(null);
    setThumbUrl(null);
    setThumbProgress(0);
    setThumbUploadStatus("idle");
    setThumbUploadError(null);
    setFiles([]);
    setExperiments([]);
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
    setFiles((prev) => [...prev, ...parsed]);
    parsed.forEach((file) => {
      void uploadParsedFileNow(file, { entryId, experimentId: null }, patchEntryFile);
    });
  };

  const addEntryUrl = async (rawUrl: string) => {
    const parsed = await parseUrlFile(rawUrl, "L0");
    if (!parsed) {
      return;
    }
    setFiles((prev) => [...prev, parsed]);
  };

  const addExperiment = () => {
    setExperiments((prev) => [
      ...prev,
      {
        id: crypto.randomUUID(),
        name: "",
        description: "",
        thumbFileId: crypto.randomUUID(),
        thumbFile: null,
        thumbPreview: null,
        thumbUrl: null,
        thumbProgress: 0,
        thumbUploadStatus: "idle",
        thumbUploadError: null,
        files: [],
        metrics: [],
      },
    ]);
  };

  const updateExperiment = (id: string, patch: Partial<ExperimentDraft>) => {
    setExperiments((prev) =>
      prev.map((exp) => (exp.id === id ? { ...exp, ...patch } : exp)),
    );
  };

  const removeExperiment = (id: string) => {
    setExperiments((prev) => prev.filter((exp) => exp.id !== id));
  };

  const patchEntryFile = (id: string, patch: Partial<ParsedFile>) => {
    setFiles((prev) =>
      prev.map((file) => (file.id === id ? { ...file, ...patch } : file)),
    );
  };

  const patchExperimentFile = (
    experimentId: string,
    fileId: string,
    patch: Partial<ParsedFile>,
  ) => {
    setExperiments((prev) =>
      prev.map((exp) =>
        exp.id === experimentId
          ? {
              ...exp,
              files: exp.files.map((file) =>
                file.id === fileId ? { ...file, ...patch } : file,
              ),
            }
          : exp,
      ),
    );
  };

  const uploadEntryThumbnail = async (file: File, fileId: string) => {
    try {
      const url = await uploadFileToObjectStorage(
        file,
        { entryId, experimentId: null, entityId: fileId },
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

  const uploadExperimentThumbnail = async (
    experimentId: string,
    file: File,
    fileId: string,
  ) => {
    const setThumbState = (patch: Partial<ExperimentDraft>) =>
      setExperiments((prev) =>
        prev.map((exp) =>
          exp.id === experimentId && exp.thumbFileId === fileId
            ? { ...exp, ...patch }
            : exp,
        ),
      );
    try {
      const url = await uploadFileToObjectStorage(
        file,
        { entryId, experimentId, entityId: fileId },
        (fraction) => setThumbState({ thumbProgress: fraction }),
      );
      setThumbState({
        thumbUrl: url,
        thumbProgress: 1,
        thumbUploadStatus: "uploaded",
        thumbUploadError: null,
      });
    } catch (error) {
      setThumbState({
        thumbUploadStatus: "failed",
        thumbUploadError: uploadErrorMessage(error),
      });
    }
  };

  const hasPendingUploads =
    thumbUploadStatus === "uploading" ||
    files.some((file) => file.uploadStatus === "uploading") ||
    experiments.some(
      (exp) =>
        exp.thumbUploadStatus === "uploading" ||
        exp.files.some((file) => file.uploadStatus === "uploading"),
    );

  const hasFailedUploads =
    thumbUploadStatus === "failed" ||
    files.some((file) => file.uploadStatus === "failed") ||
    experiments.some(
      (exp) =>
        exp.thumbUploadStatus === "failed" ||
        exp.files.some((file) => file.uploadStatus === "failed"),
    );

  const canSubmit =
    name.trim().length > 0 &&
    experiments.every((exp) => exp.name.trim().length > 0) &&
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
      if (!uploadsReady(files, experiments, thumbFile, thumbUrl)) {
        setError("Wait until all files are uploaded.");
        setSubmitting(false);
        return;
      }

      const input: CreateEntryInput = {
        id: entryId,
        name: name.trim(),
        description: description.trim() || null,
        thumbnail_image_url: thumbUrl,
        entities: files.map(toEntity),
        experiments: experiments.map((exp) => ({
          id: exp.id,
          name: exp.name.trim(),
          description: exp.description.trim() || null,
          thumbnail_image_url: exp.thumbUrl,
          entities: [...exp.files.map(toEntity), ...exp.metrics.map(toMetricEntity)],
        })),
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

      <section className={styles.field}>
        <label className={styles.label} htmlFor="entry-name">
          Name
        </label>
        <input
          id="entry-name"
          className={styles.input}
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="e.g. Hen egg-white lysozyme"
          autoComplete="off"
        />
      </section>

      <section className={styles.field}>
        <label className={styles.label} htmlFor="entry-desc">
          Description
        </label>
        <textarea
          id="entry-desc"
          className={styles.textarea}
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          placeholder="e.g. 129-residue antibacterial enzyme; common crystallography benchmark"
          rows={3}
        />
      </section>

      <section className={styles.field}>
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

      <section className={styles.field}>
        <div className={styles.expHead}>
          <span className={styles.label}>Models</span>
          <button type="button" className={styles.addExp} onClick={addExperiment}>
            <PlusIcon />
            Add model
          </button>
        </div>

        {experiments.length === 0 ? (
          <p className={styles.expEmpty}>No models yet.</p>
        ) : (
          <div className={styles.expList}>
            {experiments.map((exp, index) => (
              <div key={exp.id} className={styles.expCard}>
                <div className={styles.expCardHead}>
                  <span className={styles.expIndex}>#{index + 1}</span>
                  <button
                    type="button"
                    className={styles.remove}
                    onClick={() => removeExperiment(exp.id)}
                    aria-label="Remove model"
                  >
                    ×
                  </button>
                </div>
                <input
                  className={styles.input}
                  value={exp.name}
                  onChange={(event) =>
                    updateExperiment(exp.id, { name: event.target.value })
                  }
                  placeholder="e.g. Refined structure (REFMAC)"
                  autoComplete="off"
                />
                <input
                  className={styles.input}
                  value={exp.description}
                  onChange={(event) =>
                    updateExperiment(exp.id, { description: event.target.value })
                  }
                  placeholder="Optional — e.g. molecular replacement, then restrained refinement"
                  autoComplete="off"
                />
                <label className={styles.thumbDrop}>
                  {exp.thumbPreview ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img className={styles.thumbImg} src={exp.thumbPreview} alt="" />
                  ) : (
                    <span className={styles.thumbHint}>
                      <UploadIcon />
                      Preview image
                    </span>
                  )}
                  {exp.thumbUploadStatus === "uploading" ? (
                    <span className={styles.thumbOverlay}>
                      <ProgressBar value={exp.thumbProgress} />
                    </span>
                  ) : null}
                  <input
                    type="file"
                    accept="image/*"
                    className={styles.hiddenInput}
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      if (!file) {
                        return;
                      }
                      const nextThumbFileId = crypto.randomUUID();
                      updateExperiment(exp.id, {
                        thumbFileId: nextThumbFileId,
                        thumbFile: file,
                        thumbPreview: URL.createObjectURL(file),
                        thumbUrl: null,
                        thumbProgress: 0,
                        thumbUploadStatus: "uploading",
                        thumbUploadError: null,
                      });
                      void uploadExperimentThumbnail(
                        exp.id,
                        file,
                        nextThumbFileId,
                      );
                    }}
                  />
                </label>
                {exp.thumbUploadStatus === "failed" ? (
                  <span className={styles.uploadStatus} data-state="failed">
                    {uploadStatusText(
                      exp.thumbUploadStatus,
                      exp.thumbUploadError,
                    )}
                  </span>
                ) : null}
                <span className={styles.subLabel}>Data</span>
                <FilesEditor
                  files={exp.files}
                  onAdd={async (list) => {
                    const parsed = await Promise.all(
                      list.map((file) => parseFile(file, "L2")),
                    );
                    setExperiments((prev) =>
                      prev.map((current) =>
                        current.id === exp.id
                          ? { ...current, files: [...current.files, ...parsed] }
                          : current,
                      ),
                    );
                    parsed.forEach((file) => {
                      void uploadParsedFileNow(
                        file,
                        { entryId, experimentId: exp.id },
                        (fileId, patch) =>
                          patchExperimentFile(exp.id, fileId, patch),
                      );
                    });
                  }}
                  onAddUrl={async (rawUrl) => {
                    const parsed = await parseUrlFile(rawUrl, "L2");
                    if (!parsed) {
                      return;
                    }
                    setExperiments((prev) =>
                      prev.map((current) =>
                        current.id === exp.id
                          ? { ...current, files: [...current.files, parsed] }
                          : current,
                      ),
                    );
                  }}
                  onRemove={(id) =>
                    updateExperiment(exp.id, {
                      files: exp.files.filter((f) => f.id !== id),
                    })
                  }
                  onLevel={(id, level) =>
                    updateExperiment(exp.id, {
                      files: exp.files.map((f) =>
                        f.id === id ? { ...f, level } : f,
                      ),
                    })
                  }
                  onPatch={(id, patch) =>
                    updateExperiment(exp.id, {
                      files: exp.files.map((f) =>
                        f.id === id ? { ...f, ...patch } : f,
                      ),
                    })
                  }
                />

                <span className={styles.subLabel}>Metrics</span>
                <MetricsEditor
                  metrics={exp.metrics}
                  setMetrics={(next) =>
                    updateExperiment(exp.id, { metrics: next })
                  }
                />
              </div>
            ))}
          </div>
        )}
      </section>

      {error ? <p className={styles.error}>{error}</p> : null}

      <div className={styles.actions}>
        <button type="button" className={styles.resetBtn} onClick={resetForm}>
          Reset form
        </button>
        <div className={styles.actionsRight}>
          <a className={styles.secondary} href="/">
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
    draft.experiments.reduce((sum, exp) => sum + exp.files.length, 0);
  const parts: string[] = [];
  if (draft.name.trim()) {
    parts.push(`“${draft.name.trim()}”`);
  }
  if (fileCount > 0) {
    parts.push(`${fileCount} uploaded file${fileCount === 1 ? "" : "s"}`);
  }
  if (draft.experiments.length > 0) {
    parts.push(
      `${draft.experiments.length} model${draft.experiments.length === 1 ? "" : "s"}`,
    );
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

function FilesEditor({
  files,
  onAdd,
  onAddUrl,
  onRemove,
  onLevel,
  onPatch,
  lockLevel = false,
}: {
  files: ParsedFile[];
  onAdd: (list: File[]) => void;
  onAddUrl: (url: string) => void;
  onRemove: (id: string) => void;
  onLevel: (id: string, level: EntityLevel) => void;
  onPatch: (id: string, patch: Partial<ParsedFile>) => void;
  lockLevel?: boolean;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [linkOpen, setLinkOpen] = useState(false);
  const [linkValue, setLinkValue] = useState("");

  const onChange = (event: ChangeEvent<HTMLInputElement>) => {
    const list = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (list.length > 0) {
      onAdd(list);
    }
  };

  const submitLink = () => {
    const url = linkValue.trim();
    if (!isValidHttpUrl(url)) {
      return;
    }
    onAddUrl(url);
    setLinkValue("");
    setLinkOpen(false);
  };

  return (
    <div className={styles.filesEditor}>
      <div className={styles.addRow}>
        <button
          type="button"
          className={styles.fileDrop}
          onClick={() => inputRef.current?.click()}
        >
          <UploadIcon />
          Add data/model
          <span className={styles.fileDropHint}>
            Sequences, structures, maps — .fasta, .pdb, .cif, .ccp4, .log
          </span>
        </button>
        <button
          type="button"
          className={styles.linkToggle}
          onClick={() => setLinkOpen((open) => !open)}
          aria-expanded={linkOpen}
        >
          <LinkIcon />
          Add link
        </button>
      </div>
      <input
        ref={inputRef}
        type="file"
        multiple
        className={styles.hiddenInput}
        onChange={onChange}
      />

      {linkOpen ? (
        <div className={styles.linkRow}>
          <input
            className={styles.input}
            type="url"
            inputMode="url"
            value={linkValue}
            onChange={(event) => setLinkValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submitLink();
              }
            }}
            placeholder="e.g. https://example.org/structure.pdb"
            autoFocus
          />
          <button
            type="button"
            className={styles.linkAddBtn}
            onClick={submitLink}
            disabled={!isValidHttpUrl(linkValue.trim())}
          >
            Add
          </button>
        </div>
      ) : null}

      {files.length > 0 ? (
        <ul className={styles.fileList}>
          {files.map((file) => (
            <li key={file.id} className={styles.fileItem}>
              {file.uploadStatus === "uploading" ? (
                <div className={styles.uploadingRow}>
                  <span className={styles.fileType}>{file.type}</span>
                  <div className={styles.uploadingMeta}>
                    <span className={styles.fileName}>{file.name}</span>
                    <ProgressBar value={file.progress} />
                  </div>
                  <button
                    type="button"
                    className={styles.remove}
                    onClick={() => onRemove(file.id)}
                    aria-label={`Cancel ${file.name}`}
                  >
                    ×
                  </button>
                </div>
              ) : file.uploadStatus === "failed" ? (
                <div className={styles.fileRow}>
                  <span className={styles.fileType} data-failed="true">
                    {file.type}
                  </span>
                  <span className={styles.fileMeta}>
                    <span className={styles.fileName}>{file.name}</span>
                    <span
                      className={styles.fileUploadStatus}
                      data-state="failed"
                    >
                      {uploadStatusText(file.uploadStatus, file.uploadError)}
                    </span>
                  </span>
                  <button
                    type="button"
                    className={styles.remove}
                    onClick={() => onRemove(file.id)}
                    aria-label={`Remove ${file.name}`}
                  >
                    ×
                  </button>
                </div>
              ) : (
                <>
                  <div className={styles.fileRow}>
                    <span className={styles.fileType}>{file.type}</span>
                    <span className={styles.fileMeta}>
                      <span className={styles.fileNameRow}>
                        <span className={styles.fileName}>{file.name}</span>
                        {file.source === "url" ? (
                          <span className={styles.sourceBadge}>
                            <LinkIcon />
                            link
                          </span>
                        ) : null}
                      </span>
                      <span className={styles.fileSub}>
                        {fileEntityType(file)}
                        {file.size > 0 ? ` · ${formatSize(file.size)}` : ""}
                        {typeof file.metadata?.length === "number"
                          ? ` · ${file.metadata.length} residues`
                          : ""}
                      </span>
                    </span>
                    {lockLevel ? (
                      <span
                        className={styles.levelBadge}
                        data-level={file.level}
                        title="Baseline data is always level L0"
                      >
                        {file.level}
                      </span>
                    ) : (
                      <select
                        className={styles.levelSelect}
                        value={file.level}
                        onChange={(event) =>
                          onLevel(file.id, event.target.value as EntityLevel)
                        }
                        aria-label="Level"
                      >
                        {LEVELS.map((level) => (
                          <option key={level} value={level}>
                            {level}
                          </option>
                        ))}
                      </select>
                    )}
                    <button
                      type="button"
                      className={styles.remove}
                      onClick={() => onRemove(file.id)}
                      aria-label={`Remove ${file.name}`}
                    >
                      ×
                    </button>
                  </div>

                  <div className={styles.depositBlock}>
                    <div className={styles.depositField}>
                      <span className={styles.depositLabel}>Authors</span>
                      <AuthorsInput
                        value={file.authors}
                        onChange={(next) =>
                          onPatch(file.id, { authors: next })
                        }
                      />
                    </div>
                    <div className={styles.depositField}>
                      <span className={styles.depositLabel}>Affiliation</span>
                      <input
                        className={styles.input}
                        value={file.affiliation}
                        onChange={(event) =>
                          onPatch(file.id, {
                            affiliation: event.target.value,
                          })
                        }
                        placeholder="e.g. Department of Chemistry, Boston University"
                        autoComplete="organization"
                      />
                    </div>
                  </div>

                  {file.type === "image" && file.preview ? (
                    <div className={styles.filePreview}>
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        className={styles.filePreviewImg}
                        src={file.preview}
                        alt=""
                      />
                    </div>
                  ) : file.type === "fasta" && file.metadata ? (
                    <div className={styles.filePreview}>
                      <SequenceView metadata={file.metadata as FastaMetadata} />
                    </div>
                  ) : null}
                </>
              )}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function AuthorsInput({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const [draft, setDraft] = useState("");
  const authors = authorsTextToList(value);

  const commit = (raw: string) => {
    const author = raw.trim();
    if (!author) {
      return;
    }
    onChange([...authors, author].join("\n"));
    setDraft("");
  };

  const removeAt = (index: number) => {
    onChange(authors.filter((_, i) => i !== index).join("\n"));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      commit(draft);
    } else if (event.key === "Backspace" && draft === "" && authors.length > 0) {
      removeAt(authors.length - 1);
    }
  };

  return (
    <div className={styles.authorsInput}>
      {authors.map((author, index) => (
        <span key={`${author}-${index}`} className={styles.authorChip}>
          {author}
          <button
            type="button"
            className={styles.chipRemove}
            onClick={() => removeAt(index)}
            aria-label={`Remove ${author}`}
          >
            ×
          </button>
        </span>
      ))}
      <input
        className={styles.authorsField}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => commit(draft)}
        placeholder={
          authors.length > 0
            ? "Add another…"
            : "e.g. Hendrickson, W.A. — press Enter"
        }
      />
    </div>
  );
}

function ProgressBar({ value }: { value: number }) {
  const pct = Math.round(Math.min(1, Math.max(0, value)) * 100);
  return (
    <div className={styles.progressWrap}>
      <div className={styles.progressTrack}>
        <div className={styles.progressFill} style={{ width: `${pct}%` }} />
      </div>
      <span className={styles.progressPct}>{pct}%</span>
    </div>
  );
}

function MetricsEditor({
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
      <button type="button" className={styles.fileDrop} onClick={add}>
        <PlusIcon />
        Add metrics
      </button>

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

function toEntity(file: ParsedFile) {
  const entityType = fileEntityType(file);
  const payload: Record<string, unknown> = {
    file_url: file.url,
    ...(file.size > 0 ? { size: file.size } : {}),
    ...(file.metadata ? { metadata: file.metadata } : {}),
  };
  if (entityType === "data") {
    payload.type = file.type;
  }
  const authors = authorsTextToList(file.authors);
  if (authors.length > 0) {
    payload.authors = authors;
  }
  const affiliation = file.affiliation.trim();
  if (affiliation) {
    payload.affiliation = affiliation;
  }

  return {
    id: file.id,
    type: entityType,
    level: file.level,
    name: file.name,
    payload,
  };
}

function fileEntityType(file: ParsedFile): "data" | "model" {
  return file.type === "pdb" || file.type === "mmcif" ? "model" : "data";
}

function authorsTextToList(text: string): string[] {
  return text
    .split(/\r?\n|;/)
    .map((author) => author.trim())
    .filter(Boolean);
}

// Restrict metric input to a single decimal number. Commas are treated as the
// decimal separator and normalized to a dot, so "0,205" and "0.205" both store
// as "0.205"; anything non-numeric is dropped as you type.
function sanitizeNumeric(raw: string): string {
  let s = raw.replace(/,/g, ".").replace(/[^0-9.\-]/g, "");
  const firstDot = s.indexOf(".");
  if (firstDot !== -1) {
    s = s.slice(0, firstDot + 1) + s.slice(firstDot + 1).replace(/\./g, "");
  }
  s = s.replace(/(?!^)-/g, "");
  return s;
}

function toMetricEntity(metric: MetricDraft) {
  const payload: Record<string, number> = {};
  for (const field of METRIC_FIELDS) {
    const raw = metric.values[field.key];
    const value = raw != null && raw.trim() !== "" ? Number(raw) : NaN;
    if (!Number.isNaN(value)) {
      payload[field.key] = value;
    }
  }
  return {
    id: metric.id,
    type: "metrics" as const,
    level: "L3" as const,
    name: "Metrics",
    payload,
  };
}

async function parseFile(file: File, level: EntityLevel): Promise<ParsedFile> {
  const type = detectType(file.name);
  const base: ParsedFile = {
    id: crypto.randomUUID(),
    file,
    source: "upload",
    name: file.name,
    size: file.size,
    type,
    level,
    authors: "",
    affiliation: "",
    url: "",
    progress: 0,
    uploadStatus: "uploading",
    uploadError: null,
  };
  if (type === "fasta") {
    try {
      base.metadata = parseFasta(await file.text());
    } catch {
      // keep the file without metadata on parse failure
    }
  }
  if (type === "image") {
    base.preview = URL.createObjectURL(file);
  }
  return base;
}

// Builds a file entry from an external URL. Nothing is uploaded — the link is
// stored as-is; a small, known-format file is fetched client-side (best effort)
// to build a preview. CORS failures are swallowed and just skip the preview.
async function parseUrlFile(
  rawUrl: string,
  level: EntityLevel,
): Promise<ParsedFile | null> {
  const url = rawUrl.trim();
  if (!isValidHttpUrl(url)) {
    return null;
  }
  const name = fileNameFromUrl(url);
  const type = detectType(name);
  const base: ParsedFile = {
    id: crypto.randomUUID(),
    source: "url",
    name,
    size: 0,
    type,
    level,
    authors: "",
    affiliation: "",
    url,
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
  };

  try {
    if (type === "image") {
      base.preview = url;
    } else if (type === "fasta") {
      const response = await fetch(url);
      if (response.ok) {
        const declared = Number(response.headers.get("content-length") ?? "0");
        if (!declared || declared < 2_000_000) {
          const text = await response.text();
          if (text.length < 4_000_000) {
            base.size = text.length;
            base.metadata = parseFasta(text);
          }
        }
      }
    }
  } catch {
    // Network/CORS error — keep the link without a preview.
  }
  return base;
}

function fileNameFromUrl(url: string): string {
  try {
    const parsed = new URL(url);
    const last = parsed.pathname.split("/").filter(Boolean).pop();
    return last ? decodeURIComponent(last) : parsed.hostname;
  } catch {
    return "linked-file";
  }
}

function isValidHttpUrl(value: string): boolean {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

type FileUploadLocation = Pick<FileUploadContext, "entryId" | "experimentId">;

async function uploadParsedFileNow(
  file: ParsedFile,
  location: FileUploadLocation,
  patchFile: (id: string, patch: Partial<ParsedFile>) => void,
): Promise<void> {
  if (!file.file) {
    return;
  }
  try {
    const url = await uploadFileToObjectStorage(
      file.file,
      { ...location, entityId: file.id, filename: file.name },
      (fraction) => patchFile(file.id, { progress: fraction }),
    );
    patchFile(file.id, {
      url,
      progress: 1,
      uploadStatus: "uploaded",
      uploadError: null,
    });
  } catch (error) {
    patchFile(file.id, {
      uploadStatus: "failed",
      uploadError: uploadErrorMessage(error),
    });
  }
}

function uploadErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Upload failed.";
}

function uploadStatusText(status: UploadStatus, error: string | null): string {
  if (status === "uploading") {
    return "Uploading...";
  }
  if (status === "uploaded") {
    return "Uploaded";
  }
  if (status === "failed") {
    return error ? `Upload failed: ${error}` : "Upload failed";
  }
  return "";
}

function uploadsReady(
  files: ParsedFile[],
  experiments: ExperimentDraft[],
  thumbFile: File | null,
  thumbUrl: string | null,
): boolean {
  if (thumbFile && !thumbUrl) {
    return false;
  }
  if (files.some((file) => file.uploadStatus !== "uploaded" || !file.url)) {
    return false;
  }
  return experiments.every(
    (exp) =>
      (!exp.thumbFile || Boolean(exp.thumbUrl)) &&
      exp.files.every(
        (file) => file.uploadStatus === "uploaded" && Boolean(file.url),
      ),
  );
}

/* ------------------------------------------------------------------ *
 * Draft persistence (localStorage)
 * ------------------------------------------------------------------ */

type StoredFile = {
  id: string;
  source: FileSource;
  name: string;
  size: number;
  type: string;
  level: EntityLevel;
  authors: string;
  affiliation: string;
  metadata?: Record<string, unknown>;
  preview?: string;
  url: string;
};

type StoredExperiment = {
  id: string;
  name: string;
  description: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  files: StoredFile[];
  metrics: MetricDraft[];
};

type StoredDraft = {
  version: number;
  entryId: string;
  name: string;
  description: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  thumbUploadStatus: UploadStatus;
  files: StoredFile[];
  experiments: StoredExperiment[];
};

function isPersistable(file: ParsedFile): boolean {
  return file.uploadStatus === "uploaded" && Boolean(file.url);
}

function httpOnly(value: string | null | undefined): string | undefined {
  return value && /^https?:/i.test(value) ? value : undefined;
}

function fileToDraft(file: ParsedFile): StoredFile {
  return {
    id: file.id,
    source: file.source,
    name: file.name,
    size: file.size,
    type: file.type,
    level: file.level,
    authors: file.authors,
    affiliation: file.affiliation,
    metadata: file.metadata,
    preview: httpOnly(file.preview),
    url: file.url,
  };
}

function fileFromDraft(file: StoredFile): ParsedFile {
  return {
    id: file.id,
    source: file.source,
    name: file.name,
    size: file.size,
    type: file.type,
    level: file.level,
    authors: file.authors,
    affiliation: file.affiliation,
    metadata: file.metadata,
    preview: file.preview,
    url: file.url,
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
  };
}

function experimentToDraft(exp: ExperimentDraft): StoredExperiment {
  return {
    id: exp.id,
    name: exp.name,
    description: exp.description,
    thumbUrl: exp.thumbUrl,
    thumbPreview: httpOnly(exp.thumbPreview),
    files: exp.files.filter(isPersistable).map(fileToDraft),
    metrics: exp.metrics,
  };
}

function experimentFromDraft(exp: StoredExperiment): ExperimentDraft {
  return {
    id: exp.id,
    name: exp.name,
    description: exp.description,
    thumbFileId: crypto.randomUUID(),
    thumbFile: null,
    thumbPreview: exp.thumbPreview ?? null,
    thumbUrl: exp.thumbUrl,
    thumbProgress: exp.thumbUrl ? 1 : 0,
    thumbUploadStatus: exp.thumbUrl ? "uploaded" : "idle",
    thumbUploadError: null,
    files: exp.files.map(fileFromDraft),
    metrics: exp.metrics,
  };
}

function draftHasContent(draft: StoredDraft): boolean {
  return (
    draft.name.trim().length > 0 ||
    draft.description.trim().length > 0 ||
    Boolean(draft.thumbUrl) ||
    draft.files.length > 0 ||
    draft.experiments.length > 0
  );
}

function readStoredDraft(): StoredDraft | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    const raw = window.localStorage.getItem(DRAFT_STORAGE_KEY);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as StoredDraft;
    if (!parsed || parsed.version !== DRAFT_VERSION || !parsed.entryId) {
      return null;
    }
    parsed.files = Array.isArray(parsed.files) ? parsed.files : [];
    parsed.experiments = Array.isArray(parsed.experiments)
      ? parsed.experiments
      : [];
    return parsed;
  } catch {
    return null;
  }
}

function writeStoredDraft(draft: StoredDraft): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(DRAFT_STORAGE_KEY, JSON.stringify(draft));
  } catch {
    // Storage full or unavailable — drafts are best effort.
  }
}

function clearStoredDraft(): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.removeItem(DRAFT_STORAGE_KEY);
  } catch {
    // ignore
  }
}

function detectType(filename: string): string {
  const ext = filename.split(".").pop()?.toLowerCase() ?? "";
  const map: Record<string, string> = {
    fasta: "fasta",
    fa: "fasta",
    faa: "fasta",
    seq: "fasta",
    pdb: "pdb",
    cif: "mmcif",
    mmcif: "mmcif",
    ccp4: "ccp4",
    mrc: "mrc",
    map: "ccp4",
    mtz: "mtz",
    h5: "hdf5",
    hdf5: "hdf5",
    cbf: "cbf",
    png: "image",
    jpg: "image",
    jpeg: "image",
    log: "log",
    txt: "text",
    json: "json",
    csv: "csv",
  };
  return map[ext] ?? ext ?? "file";
}

function parseFasta(text: string): Record<string, unknown> {
  const lines = text.split(/\r?\n/);
  let chains = 0;
  let sequence = "";
  for (const line of lines) {
    if (line.startsWith(">")) {
      chains += 1;
      continue;
    }
    sequence += line.trim();
  }
  sequence = sequence.replace(/\s+/g, "").toUpperCase();
  return { chains: chains || 1, length: sequence.length, sequence };
}

function formatSize(size: number): string {
  if (size <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 ? value : Math.round(value * 10) / 10;
  return `${rounded} ${units[unit]}`;
}

function UploadIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 16V4m0 0 4 4m-4-4-4 4M5 20h14" />
    </svg>
  );
}

function LinkIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.5 1.5" />
      <path d="M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.5-1.5" />
    </svg>
  );
}

function PlusIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}
