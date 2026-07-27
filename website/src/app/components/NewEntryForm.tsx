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
  CreateEntityInput,
  CreateEntryInput,
  CreateEntityRelationInput,
  CreateModelInput,
  EntityLevel,
  FastaMetadata,
} from "@/lib/api/entries";
import {
  extFileKey,
  extFileReferenceURL,
  getExtExperiment,
  parseExtExperimentRef,
  searchExtFiles,
  type ExtExperiment,
  type ExtFile,
  type ExtFileReference,
} from "@/lib/api/ext";
import {
  uploadFileToObjectStorage,
  type FileUploadContext,
} from "@/lib/api/uploads";
import { createEntryAction } from "@/app/entries/new/actions";
import SequenceView from "./SequenceView";

import styles from "./NewEntryForm.module.css";

export type UploadStatus = "idle" | "uploading" | "uploaded" | "failed";

export type FileSource = "upload" | "url" | "ext";

export type ParsedFile = {
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
  extReference?: ExtFileReference;
};

export type MetricDraft = {
  id: string;
  values: Record<string, string>;
};

export type ProgramDraft = {
  id: string;
  name: string;
  version: string;
  description: string;
};

export type ModelDraft = {
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
  program: ProgramDraft | null;
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
  const [models, setModels] = useState<ModelDraft[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
        setName((current) => current || experiment.name);
        setDescription((current) => current || experiment.description);
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
      name,
      description,
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
    name,
    description,
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
    setName(draft.name);
    setDescription(draft.description);
    setThumbUrl(draft.thumbUrl);
    setThumbPreview(draft.thumbPreview ?? null);
    setThumbUploadStatus(draft.thumbUrl ? "uploaded" : "idle");
    setThumbProgress(draft.thumbUrl ? 1 : 0);
    setFiles(draft.files.map(fileFromDraft));
    setModels(draft.models.map(modelFromDraft));
    setDraftPrompt(null);
    setStorageReady(true);
  };

  const discardDraft = () => {
    clearStoredDraft();
    setDraftPrompt(null);
    setStorageReady(true);
    // "Start fresh" means a blank form: drop the linked experiment along with
    // the name and description it prefilled.
    unlinkExtExperiment();
    setName("");
    setDescription("");
  };

  const resetForm = () => {
    clearStoredDraft();
    setEntryId(crypto.randomUUID());
    // The link is something the user entered, so it resets with everything
    // else instead of re-seeding name and description from the experiment.
    unlinkExtExperiment();
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
    setFiles((prev) => [...prev, ...parsed]);
    parsed.forEach((file) => {
      void uploadParsedFileNow(file, { entryId, modelId: null }, patchEntryFile);
    });
  };

  const addEntryUrl = async (rawUrl: string) => {
    const parsed = await parseUrlFile(rawUrl, "L0");
    if (!parsed) {
      return;
    }
    setFiles((prev) => [...prev, parsed]);
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
    setFiles((prev) => [...prev, ...parsed]);
  };

  const addModel = () => {
    setModels((prev) => [
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
        program: null,
      },
    ]);
  };

  const updateModel = (id: string, patch: Partial<ModelDraft>) => {
    setModels((prev) =>
      prev.map((modelDraft) => (modelDraft.id === id ? { ...modelDraft, ...patch } : modelDraft)),
    );
  };

  const removeModel = (id: string) => {
    setModels((prev) => prev.filter((modelDraft) => modelDraft.id !== id));
  };

  const patchEntryFile = (id: string, patch: Partial<ParsedFile>) => {
    setFiles((prev) =>
      prev.map((file) => (file.id === id ? { ...file, ...patch } : file)),
    );
  };

  const patchModelFile = (
    modelId: string,
    fileId: string,
    patch: Partial<ParsedFile>,
  ) => {
    setModels((prev) =>
      prev.map((modelDraft) =>
        modelDraft.id === modelId
          ? {
              ...modelDraft,
              files: modelDraft.files.map((file) =>
                file.id === fileId ? { ...file, ...patch } : file,
              ),
            }
          : modelDraft,
      ),
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

  const uploadModelThumbnail = async (
    modelId: string,
    file: File,
    fileId: string,
  ) => {
    const setThumbState = (patch: Partial<ModelDraft>) =>
      setModels((prev) =>
        prev.map((modelDraft) =>
          modelDraft.id === modelId && modelDraft.thumbFileId === fileId
            ? { ...modelDraft, ...patch }
            : modelDraft,
        ),
      );
    try {
      const url = await uploadFileToObjectStorage(
        file,
        { entryId, modelId, entityId: fileId },
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
    name.trim().length > 0 &&
    models.every(
      (modelDraft) =>
        modelDraft.name.trim().length > 0 && !modelValidationMessage(modelDraft),
    ) &&
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
      if (!uploadsReady(files, models, thumbFile, thumbUrl)) {
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
        models: models.map(buildCreateModelInput),
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
          data-1p-ignore
          data-lpignore="true"
          data-form-type="other"
        />
        <ExtSourceField
          experiment={extExperiment}
          error={extExperimentError}
          loading={extExperimentLoading}
          onLink={linkExtExperiment}
          onUnlink={unlinkExtExperiment}
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
                  return validationMessage ? (
                    <p className={styles.inlineError}>{validationMessage}</p>
                  ) : null;
                })()}
                <div className={styles.modelDraftCardHead}>
                  <span className={styles.modelDraftIndex}>#{index + 1}</span>
                  <button
                    type="button"
                    className={styles.remove}
                    onClick={() => removeModel(modelDraft.id)}
                    aria-label="Remove model"
                  >
                    ×
                  </button>
                </div>
                <input
                  className={styles.input}
                  value={modelDraft.name}
                  onChange={(event) =>
                    updateModel(modelDraft.id, { name: event.target.value })
                  }
                  placeholder="e.g. Refined structure (REFMAC)"
                  autoComplete="off"
                />
                <input
                  className={styles.input}
                  value={modelDraft.description}
                  onChange={(event) =>
                    updateModel(modelDraft.id, { description: event.target.value })
                  }
                  placeholder="Optional — e.g. molecular replacement, then restrained refinement"
                  autoComplete="off"
                />
                <label className={styles.thumbDrop}>
                  {modelDraft.thumbPreview ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img className={styles.thumbImg} src={modelDraft.thumbPreview} alt="" />
                  ) : (
                    <span className={styles.thumbHint}>
                      <UploadIcon />
                      Preview image
                    </span>
                  )}
                  {modelDraft.thumbUploadStatus === "uploading" ? (
                    <span className={styles.thumbOverlay}>
                      <ProgressBar value={modelDraft.thumbProgress} />
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
                      updateModel(modelDraft.id, {
                        thumbFileId: nextThumbFileId,
                        thumbFile: file,
                        thumbPreview: URL.createObjectURL(file),
                        thumbUrl: null,
                        thumbProgress: 0,
                        thumbUploadStatus: "uploading",
                        thumbUploadError: null,
                      });
                      void uploadModelThumbnail(
                        modelDraft.id,
                        file,
                        nextThumbFileId,
                      );
                    }}
                  />
                </label>
                {modelDraft.thumbUploadStatus === "failed" ? (
                  <span className={styles.uploadStatus} data-state="failed">
                    {uploadStatusText(
                      modelDraft.thumbUploadStatus,
                      modelDraft.thumbUploadError,
                    )}
                  </span>
                ) : null}
                <span className={styles.subLabel}>Data</span>
                <FilesEditor
                  files={modelDraft.files}
                  lockModelLevel
                  extExperiment={extExperiment}
                  onAdd={async (list) => {
                    const parsed = await Promise.all(
                      list.map((file) => parseModelFile(file)),
                    );
                    if (!canAddModelFiles(modelDraft.files, parsed)) {
                      setError("Each model must contain exactly one PDB/mmCIF model file.");
                      return;
                    }
                    setError(null);
                    setModels((prev) =>
                      prev.map((current) =>
                        current.id === modelDraft.id
                          ? { ...current, files: [...current.files, ...parsed] }
                          : current,
                      ),
                    );
                    parsed.forEach((file) => {
                      void uploadParsedFileNow(
                        file,
                        { entryId, modelId: modelDraft.id },
                        (fileId, patch) =>
                          patchModelFile(modelDraft.id, fileId, patch),
                      );
                    });
                  }}
                  onAddUrl={async (rawUrl) => {
                    const parsed = await parseModelUrlFile(rawUrl);
                    if (!parsed) {
                      return;
                    }
                    if (!canAddModelFiles(modelDraft.files, [parsed])) {
                      setError("Each model must contain exactly one PDB/mmCIF model file.");
                      return;
                    }
                    setError(null);
                    setModels((prev) =>
                      prev.map((current) =>
                        current.id === modelDraft.id
                          ? { ...current, files: [...current.files, parsed] }
                          : current,
                      ),
                    );
                  }}
                  onAddExt={(selected) => {
                    if (!extExperiment) {
                      return;
                    }
                    const existing = extFileKeys(modelDraft.files);
                    const parsed = selected
                      .filter((file) => !existing.has(extFileKey(file)))
                      .map((file) =>
                        normalizeModelLevel(
                          extFileToParsed(file, extExperiment.id, "L2"),
                        ),
                      );
                    if (parsed.length === 0) {
                      return;
                    }
                    if (!canAddModelFiles(modelDraft.files, parsed)) {
                      setError(
                        "Each model must contain exactly one PDB/mmCIF model file.",
                      );
                      return;
                    }
                    setError(null);
                    setModels((prev) =>
                      prev.map((current) =>
                        current.id === modelDraft.id
                          ? {
                              ...current,
                              files: [...current.files, ...parsed],
                            }
                          : current,
                      ),
                    );
                  }}
                  onRemove={(id) =>
                    updateModel(modelDraft.id, {
                      files: modelDraft.files.filter((f) => f.id !== id),
                    })
                  }
                  onLevel={(id, level) =>
                    updateModel(modelDraft.id, {
                      files: modelDraft.files.map((f) =>
                        f.id === id ? setFileLevel(f, level) : f,
                      ),
                    })
                  }
                  onPatch={(id, patch) =>
                    updateModel(modelDraft.id, {
                      files: modelDraft.files.map((f) =>
                        f.id === id ? { ...f, ...patch } : f,
                      ),
                    })
                  }
                />

                <span className={styles.subLabel}>Metrics</span>
                <MetricsEditor
                  metrics={modelDraft.metrics}
                  setMetrics={(next) =>
                    updateModel(modelDraft.id, { metrics: next })
                  }
                />

                <span className={styles.subLabel}>Program</span>
                <ProgramEditor
                  program={modelDraft.program}
                  setProgram={(next) =>
                    updateModel(modelDraft.id, { program: next })
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

const EXT_LINK_HINT =
  "Its files become available under Baseline data, and name and description are prefilled.";
const EXT_LINK_INVALID = "That doesn't look like an Ext experiment link.";

function ExtSourceField({
  experiment,
  error,
  loading,
  onLink,
  onUnlink,
}: {
  experiment: ExtExperiment | null;
  error: string | null;
  loading: boolean;
  onLink: (experimentId: string) => void;
  onUnlink: () => void;
}) {
  // Most entries have no Ext experiment, so the field stays collapsed to a
  // single line until it is asked for.
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [parseError, setParseError] = useState<string | null>(null);
  // A failed lookup lives in the parent; hide it as soon as the user starts
  // editing so a stale message doesn't sit under the field they're fixing.
  const [errorDismissed, setErrorDismissed] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (experiment) {
      setOpen(false);
      setValue("");
      setParseError(null);
    }
  }, [experiment]);

  useEffect(() => {
    setErrorDismissed(false);
  }, [error]);

  const link = (raw: string) => {
    const experimentId = parseExtExperimentRef(raw);
    if (!experimentId) {
      setParseError(EXT_LINK_INVALID);
      return;
    }
    setParseError(null);
    onLink(experimentId);
  };

  const collapse = () => {
    setOpen(false);
    setValue("");
    setParseError(null);
    setErrorDismissed(true);
  };

  if (experiment) {
    return (
      <div className={styles.sourceSection}>
        <div className={styles.sourceRow}>
          <span className={styles.sourceMark}>Ext</span>
          <span className={styles.sourceMeta}>
            <strong>{experiment.name}</strong>
            <span>{experiment.id}</span>
          </span>
          <a
            className={styles.sourceLink}
            href={experiment.web_url}
            target="_blank"
            rel="noreferrer"
          >
            Open
          </a>
          <button
            type="button"
            className={styles.remove}
            onClick={onUnlink}
            aria-label="Unlink source experiment"
            title="Unlink source experiment"
          >
            ×
          </button>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className={styles.sourceSection}>
        <div className={styles.sourceRow} aria-busy="true">
          <span className={styles.sourceMark}>Ext</span>
          <span className={styles.sourceMeta}>
            <strong>Loading experiment…</strong>
          </span>
        </div>
      </div>
    );
  }

  const message = parseError ?? (errorDismissed ? null : error);

  // A link that arrived broken (bad query param) opens the field on its own so
  // the message has something to sit under.
  if (!open && !message) {
    return (
      <div className={styles.sourceSection}>
        <button
          type="button"
          className={styles.sourceToggle}
          onClick={() => {
            setOpen(true);
            // The field only exists once expanded, so focus it on the next tick.
            window.requestAnimationFrame(() => inputRef.current?.focus());
          }}
        >
          <PlusIcon />
          Link an Ext experiment
        </button>
      </div>
    );
  }

  return (
    <div className={styles.sourceSection}>
      <div className={styles.sourceInputRow}>
        <input
          ref={inputRef}
          id="ext-experiment-link"
          aria-label="Ext experiment link"
          className={styles.input}
          type="text"
          inputMode="url"
          value={value}
          aria-invalid={message ? true : undefined}
          aria-describedby="ext-experiment-note"
          onChange={(event) => {
            setValue(event.target.value);
            setParseError(null);
            setErrorDismissed(true);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              link(value);
            } else if (event.key === "Escape") {
              collapse();
            }
          }}
          onPaste={(event) => {
            // Pasting a link is the whole point of this field, so resolve it
            // right away instead of making the user also press Link.
            const pasted = event.clipboardData.getData("text");
            if (parseExtExperimentRef(pasted)) {
              event.preventDefault();
              setValue(pasted.trim());
              link(pasted);
            }
          }}
          placeholder="https://extshell.org/experiments/6d95a158-…"
          autoComplete="off"
          data-1p-ignore
          data-lpignore="true"
          data-form-type="other"
        />
        <button
          type="button"
          className={styles.sourceLinkBtn}
          onClick={() => link(value)}
          disabled={value.trim() === ""}
        >
          Link
        </button>
        <button
          type="button"
          className={styles.remove}
          onClick={collapse}
          aria-label="Cancel linking an experiment"
        >
          ×
        </button>
      </div>
      {message ? (
        <span
          className={styles.sourceError}
          id="ext-experiment-note"
          role="alert"
        >
          {message}
        </span>
      ) : (
        <span className={styles.sourceHint} id="ext-experiment-note">
          {EXT_LINK_HINT}
        </span>
      )}
    </div>
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
  if (draft.name.trim()) {
    parts.push(`“${draft.name.trim()}”`);
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

function FilesEditor({
  files,
  onAdd,
  onAddUrl,
  onAddExt,
  onRemove,
  onLevel,
  onPatch,
  extExperiment = null,
  lockLevel = false,
  lockModelLevel = false,
}: {
  files: ParsedFile[];
  onAdd: (list: File[]) => void;
  onAddUrl: (url: string) => void;
  onAddExt?: (files: ExtFile[]) => void;
  onRemove: (id: string) => void;
  onLevel: (id: string, level: EntityLevel) => void;
  onPatch: (id: string, patch: Partial<ParsedFile>) => void;
  extExperiment?: ExtExperiment | null;
  lockLevel?: boolean;
  lockModelLevel?: boolean;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [linkOpen, setLinkOpen] = useState(false);
  const [linkValue, setLinkValue] = useState("");
  const [extPickerOpen, setExtPickerOpen] = useState(false);
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    if (!extExperiment) {
      setExtPickerOpen(false);
    }
  }, [extExperiment]);

  // A file dropped just outside the zone would otherwise make the browser
  // navigate to it and take the half-filled form with it.
  useEffect(() => {
    const swallow = (event: globalThis.DragEvent) => {
      if (hasDraggedFiles(event.dataTransfer)) {
        event.preventDefault();
      }
    };
    window.addEventListener("dragover", swallow);
    window.addEventListener("drop", swallow);
    return () => {
      window.removeEventListener("dragover", swallow);
      window.removeEventListener("drop", swallow);
    };
  }, []);

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
      <button
        type="button"
        className={styles.dropZone}
        data-dragging={dragging ? "true" : undefined}
        onClick={() => inputRef.current?.click()}
        onDragOver={(event) => {
          if (!hasDraggedFiles(event.dataTransfer)) {
            return;
          }
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={(event) => {
          // Children of the zone fire dragleave too; ignore those so the
          // highlight doesn't flicker while moving across the label.
          const next = event.relatedTarget;
          if (next instanceof Node && event.currentTarget.contains(next)) {
            return;
          }
          setDragging(false);
        }}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          const list = droppedFiles(event.dataTransfer);
          if (list.length > 0) {
            onAdd(list);
          }
        }}
      >
        <UploadIcon />
        <span className={styles.dropZoneTitle}>
          Drop files here or <span className={styles.dropZoneBrowse}>browse</span>
        </span>
        <span className={styles.dropZoneHint}>
          Sequences, structures, maps — .fasta, .pdb, .cif, .ccp4, .log
        </span>
      </button>

      <div className={styles.altSourceRow}>
        <span className={styles.altSourceLabel}>or add from</span>
        <button
          type="button"
          className={styles.altSource}
          onClick={() => setLinkOpen((open) => !open)}
          aria-expanded={linkOpen}
          aria-label="Add a file from a URL"
        >
          <LinkIcon />
          URL
        </button>
        {extExperiment && onAddExt ? (
          <button
            type="button"
            className={styles.altSource}
            onClick={() => setExtPickerOpen(true)}
            aria-label="Add files from the linked Ext experiment"
          >
            <span className={styles.extTag}>Ext</span>
            experiment files
          </button>
        ) : null}
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

      {extPickerOpen && extExperiment && onAddExt ? (
        <ExtFilePicker
          experiment={extExperiment}
          existingKeys={extFileKeys(files)}
          onAdd={(selected) => {
            onAddExt(selected);
            setExtPickerOpen(false);
          }}
          onClose={() => setExtPickerOpen(false)}
        />
      ) : null}

      {files.length > 0 ? (
        <ul className={styles.fileList}>
          {files.map((file) => (
            <li key={file.id} className={styles.fileItem}>
              {(() => {
                const modelLevelLocked = lockModelLevel && isModelFile(file);
                const levelLocked = lockLevel || modelLevelLocked;
                const lockedLevelTitle = modelLevelLocked
                  ? "Model files are always level L2"
                  : "Baseline data is always level L0";
                return file.uploadStatus === "uploading" ? (
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
                        {uploadStatusText(
                          file.uploadStatus,
                          file.uploadError,
                        )}
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
                          ) : file.source === "ext" ? (
                            <span className={styles.sourceBadge}>Ext</span>
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
                      {levelLocked ? (
                        <span
                          className={styles.levelBadge}
                          data-level={modelLevelLocked ? "L2" : file.level}
                          title={lockedLevelTitle}
                        >
                          {modelLevelLocked ? "L2" : file.level}
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
                );
              })()}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function ExtFilePicker({
  experiment,
  existingKeys,
  onAdd,
  onClose,
}: {
  experiment: ExtExperiment;
  existingKeys: Set<string>;
  onAdd: (files: ExtFile[]) => void;
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const [items, setItems] = useState<ExtFile[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [selected, setSelected] = useState<Map<string, ExtFile>>(
    () => new Map(),
  );
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const activeQuery = useRef(query);
  activeQuery.current = query;

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setLoading(true);
      setSearchError(null);
      void searchExtFiles(experiment.id, {
        query,
        signal: controller.signal,
      })
        .then((page) => {
          setItems(page.items);
          setNextCursor(page.next_cursor);
        })
        .catch((error) => {
          if (controller.signal.aborted) {
            return;
          }
          setItems([]);
          setNextCursor(undefined);
          setSearchError(
            error instanceof Error ? error.message : "Could not search files.",
          );
        })
        .finally(() => {
          if (!controller.signal.aborted) {
            setLoading(false);
          }
        });
    }, 250);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [experiment.id, query]);

  useEffect(() => {
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [onClose]);

  const toggle = (file: ExtFile) => {
    const key = extFileKey(file);
    setSelected((current) => {
      const next = new Map(current);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.set(key, file);
      }
      return next;
    });
  };

  const loadMore = async () => {
    if (!nextCursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    setSearchError(null);
    const requestedQuery = query;
    try {
      const page = await searchExtFiles(experiment.id, {
        query: requestedQuery,
        cursor: nextCursor,
      });
      if (activeQuery.current !== requestedQuery) {
        return;
      }
      setItems((current) => [...current, ...page.items]);
      setNextCursor(page.next_cursor);
    } catch (error) {
      setSearchError(
        error instanceof Error ? error.message : "Could not load more files.",
      );
    } finally {
      setLoadingMore(false);
    }
  };

  return (
    <div
      className={styles.extPickerBackdrop}
      role="presentation"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) {
          onClose();
        }
      }}
    >
      <div
        className={styles.extPicker}
        role="dialog"
        aria-modal="true"
        aria-labelledby="ext-picker-title"
      >
        <div className={styles.extPickerHeader}>
          <span className={styles.extPickerTitleWrap}>
            <span className={styles.sourceMark}>Ext</span>
            <span>
              <strong id="ext-picker-title">Choose experiment files</strong>
              <span>{experiment.name}</span>
            </span>
          </span>
          <button
            type="button"
            className={styles.remove}
            onClick={onClose}
            aria-label="Close file picker"
          >
            ×
          </button>
        </div>

        <label className={styles.extSearch}>
          <SearchIcon />
          <input
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setNextCursor(undefined);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
              }
            }}
            placeholder="Search by path, SHA-256, or label"
            autoFocus
          />
        </label>

        <div className={styles.extResults}>
          {loading ? (
            <p className={styles.extEmpty}>Searching…</p>
          ) : items.length === 0 ? (
            <p className={styles.extEmpty}>
              {searchError ?? "No matching files."}
            </p>
          ) : (
            <ul className={styles.extResultList}>
              {items.map((file) => {
                const key = extFileKey(file);
                const alreadyAdded = existingKeys.has(key);
                const disabled = alreadyAdded || !file.available;
                return (
                  <li key={key} className={styles.extResultItem}>
                    <label data-disabled={disabled ? "true" : undefined}>
                      <input
                        type="checkbox"
                        checked={selected.has(key) || alreadyAdded}
                        disabled={disabled}
                        onChange={() => toggle(file)}
                      />
                      <span className={styles.fileType}>
                        {detectType(file.name || file.path)}
                      </span>
                      <span className={styles.extResultMeta}>
                        <strong>
                          {file.name ||
                            file.path.split("/").filter(Boolean).pop() ||
                            "Ext file"}
                        </strong>
                        <span>{file.path}</span>
                      </span>
                      <span className={styles.extResultSide}>
                        {alreadyAdded
                          ? "Added"
                          : file.directory
                            ? "Directory"
                            : !file.available
                              ? file.error || "Unavailable"
                              : formatSize(file.size)}
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <div className={styles.extPickerFooter}>
          <span>
            {selected.size === 0
              ? "No files selected"
              : `${selected.size} selected`}
          </span>
          <div className={styles.extPickerActions}>
            {nextCursor ? (
              <button
                type="button"
                className={styles.secondary}
                onClick={() => void loadMore()}
                disabled={loadingMore}
              >
                {loadingMore ? "Loading…" : "Load more"}
              </button>
            ) : null}
            <button
              type="button"
              className={styles.primary}
              disabled={selected.size === 0}
              onClick={() => onAdd(Array.from(selected.values()))}
            >
              Add selected
            </button>
          </div>
        </div>
      </div>
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

function ProgramEditor({
  program,
  setProgram,
}: {
  program: ProgramDraft | null;
  setProgram: (next: ProgramDraft | null) => void;
}) {
  const add = () =>
    setProgram({
      id: crypto.randomUUID(),
      name: "",
      version: "",
      description: "",
    });
  const patch = (next: Partial<ProgramDraft>) => {
    if (!program) {
      return;
    }
    setProgram({ ...program, ...next });
  };

  return (
    <div className={styles.filesEditor}>
      {!program ? (
        <button type="button" className={styles.fileDrop} onClick={add}>
          <PlusIcon />
          Add program
        </button>
      ) : (
        <div className={styles.metricCard}>
          <div className={styles.metricTop}>
            <span className={styles.programTag}>Program</span>
            <button
              type="button"
              className={styles.remove}
              onClick={() => setProgram(null)}
              aria-label="Remove program"
            >
              ×
            </button>
          </div>
          <div className={styles.programGrid}>
            <label className={styles.metricField}>
              <span>Name</span>
              <input
                className={styles.input}
                value={program.name}
                onChange={(event) => patch({ name: event.target.value })}
                placeholder="e.g. phenix.refine"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
            </label>
            <label className={styles.metricField}>
              <span>Version</span>
              <input
                className={styles.input}
                value={program.version}
                onChange={(event) => patch({ version: event.target.value })}
                placeholder="e.g. 1.21.2"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
            </label>
            <label className={`${styles.metricField} ${styles.programDescription}`}>
              <span>Description</span>
              <textarea
                className={styles.textarea}
                value={program.description}
                onChange={(event) =>
                  patch({ description: event.target.value })
                }
                placeholder="e.g. Reciprocal-space refinement against processed reflections"
                rows={2}
              />
            </label>
          </div>
        </div>
      )}
    </div>
  );
}

export function toEntity(file: ParsedFile) {
  const entityType = fileEntityType(file);
  const metadata = file.extReference
    ? {
        ...(file.metadata ?? {}),
        _dynamic_pdb_source: {
          provider: "ext",
          experiment_id: file.extReference.experimentId,
          path: file.extReference.path,
          sha256: file.extReference.sha256,
        },
      }
    : file.metadata;
  const payload: Record<string, unknown> = {
    file_url: file.url,
    ...(file.size > 0 ? { size: file.size } : {}),
    ...(metadata ? { metadata } : {}),
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
    level: entityType === "model" ? "L2" : file.level,
    name: file.name,
    payload,
  };
}

export function buildCreateModelInput(modelDraft: ModelDraft): CreateModelInput {
  const modelEntityFiles = modelDraft.files.filter(isModelFile);
  if (modelEntityFiles.length !== 1) {
    throw new Error("Each model must contain exactly one PDB/mmCIF model file.");
  }
  const programMessage = programValidationMessage(modelDraft.program);
  if (programMessage) {
    throw new Error(programMessage);
  }

  const fileEntities = modelDraft.files.map(toEntity);
  const metricsEntities = modelDraft.metrics.map(toMetricEntity);
  const programEntity = modelDraft.program
    ? toProgramEntity(modelDraft.program)
    : null;
  const entities: CreateEntityInput[] = [
    ...fileEntities,
    ...metricsEntities,
    ...(programEntity ? [programEntity] : []),
  ];
  const canonicalModelEntity = fileEntities.find(
    (entity) => entity.id === modelEntityFiles[0].id,
  );
  if (!canonicalModelEntity) {
    throw new Error("Each model must contain exactly one PDB/mmCIF model file.");
  }

  const relations: CreateEntityRelationInput[] = metricsEntities.map(
    (metricEntity) => ({
      source_entity_id: metricEntity.id,
      target_entity_id: canonicalModelEntity.id,
      relation_type: "metrics_for",
    }),
  );
  if (programEntity) {
    for (const entity of fileEntities) {
      if (entity.type === "data") {
        relations.push({
          source_entity_id: entity.id,
          target_entity_id: programEntity.id,
          relation_type: "input_to",
        });
      }
    }
    relations.push({
      source_entity_id: canonicalModelEntity.id,
      target_entity_id: programEntity.id,
      relation_type: "output_of",
    });
  }

  return {
    id: modelDraft.id,
    name: modelDraft.name.trim(),
    description: modelDraft.description.trim() || null,
    thumbnail_image_url: modelDraft.thumbUrl,
    entities,
    relations,
  };
}

export function modelValidationMessage(modelDraft: ModelDraft): string | null {
  const modelEntityCount = modelDraft.files.filter(isModelFile).length;
  if (modelEntityCount === 0) {
    return "Add exactly one PDB/mmCIF model file.";
  }
  if (modelEntityCount > 1) {
    return "Keep only one PDB/mmCIF model file.";
  }
  return programValidationMessage(modelDraft.program);
}

export function programValidationMessage(program: ProgramDraft | null): string | null {
  if (!program) {
    return null;
  }
  if (
    !program.name.trim() ||
    !program.version.trim() ||
    !program.description.trim()
  ) {
    return "Fill program name, version, and description or remove the program.";
  }
  return null;
}

export function toProgramEntity(program: ProgramDraft): CreateEntityInput {
  const name = program.name.trim();
  return {
    id: program.id,
    type: "program",
    level: null,
    name,
    payload: {
      name,
      version: program.version.trim(),
      description: program.description.trim(),
    },
  };
}

export function fileEntityType(file: ParsedFile): "data" | "model" {
  return file.type === "pdb" || file.type === "mmcif" ? "model" : "data";
}

export function isModelFile(file: ParsedFile): boolean {
  return fileEntityType(file) === "model";
}

export function canAddModelFiles(
  currentFiles: ParsedFile[],
  nextFiles: ParsedFile[],
): boolean {
  return (
    currentFiles.filter(isModelFile).length +
      nextFiles.filter(isModelFile).length <=
    1
  );
}

export function setFileLevel(file: ParsedFile, level: EntityLevel): ParsedFile {
  return {
    ...file,
    level: isModelFile(file) ? "L2" : level,
  };
}

export function authorsTextToList(text: string): string[] {
  return text
    .split(/\r?\n|;/)
    .map((author) => author.trim())
    .filter(Boolean);
}

// Restrict metric input to a single decimal number. Commas are treated as the
// decimal separator and normalized to a dot, so "0,205" and "0.205" both store
// as "0.205"; anything non-numeric is dropped as you type.
export function sanitizeNumeric(raw: string): string {
  let s = raw.replace(/,/g, ".").replace(/[^0-9.\-]/g, "");
  const firstDot = s.indexOf(".");
  if (firstDot !== -1) {
    s = s.slice(0, firstDot + 1) + s.slice(firstDot + 1).replace(/\./g, "");
  }
  s = s.replace(/(?!^)-/g, "");
  return s;
}

export function toMetricEntity(metric: MetricDraft) {
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

export async function parseFile(file: File, level: EntityLevel): Promise<ParsedFile> {
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

export async function parseModelFile(file: File): Promise<ParsedFile> {
  return normalizeModelLevel(await parseFile(file, "L2"));
}

export function extFileToParsed(
  file: ExtFile,
  experimentId: string,
  level: EntityLevel,
): ParsedFile {
  return {
    id: crypto.randomUUID(),
    source: "ext",
    name: file.name || file.path.split("/").filter(Boolean).pop() || "ext-file",
    size: file.size,
    type: detectType(file.name || file.path),
    level,
    authors: "",
    affiliation: "",
    metadata: file.metadata,
    url: extFileReferenceURL(experimentId, file),
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
    extReference: {
      experimentId,
      path: file.path,
      sha256: file.sha256,
    },
  };
}

export function extFileKeys(files: ParsedFile[]): Set<string> {
  return new Set(
    files.flatMap((file) =>
      file.extReference
        ? [
            extFileKey({
              path: file.extReference.path,
              sha256: file.extReference.sha256,
            }),
          ]
        : [],
    ),
  );
}

// Builds a file entry from an external URL. Nothing is uploaded — the link is
// stored as-is; a small, known-format file is fetched client-side (best effort)
// to build a preview. CORS failures are swallowed and just skip the preview.
export async function parseUrlFile(
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

export async function parseModelUrlFile(
  rawUrl: string,
): Promise<ParsedFile | null> {
  const parsed = await parseUrlFile(rawUrl, "L2");
  return parsed ? normalizeModelLevel(parsed) : null;
}

export function normalizeModelLevel(file: ParsedFile): ParsedFile {
  return isModelFile(file) ? { ...file, level: "L2" } : file;
}

export function fileNameFromUrl(url: string): string {
  try {
    const parsed = new URL(url);
    const last = parsed.pathname.split("/").filter(Boolean).pop();
    return last ? decodeURIComponent(last) : parsed.hostname;
  } catch {
    return "linked-file";
  }
}

// Only claim a drag when it actually carries files, so dragging selected text
// or a link across the form doesn't light up the drop zone.
export function hasDraggedFiles(transfer: DataTransfer | null): boolean {
  if (!transfer) {
    return false;
  }
  const types = Array.from(transfer.types ?? []);
  return types.includes("Files");
}

// Dropping a folder yields a 0-byte File that would fail to upload, so keep
// only real files. The entries API is best-effort; without it, size is the
// only signal available.
export function droppedFiles(transfer: DataTransfer | null): File[] {
  if (!transfer) {
    return [];
  }
  const items = Array.from(transfer.items ?? []);
  const directoryNames = new Set(
    items.flatMap((item) => {
      const entry = item.webkitGetAsEntry?.();
      return entry && !entry.isFile ? [entry.name] : [];
    }),
  );
  return Array.from(transfer.files ?? []).filter(
    (file) => !directoryNames.has(file.name),
  );
}

export function isValidHttpUrl(value: string): boolean {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

type FileUploadLocation = Pick<FileUploadContext, "entryId" | "modelId">;

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

export function uploadStatusText(status: UploadStatus, error: string | null): string {
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

export function uploadsReady(
  files: ParsedFile[],
  models: ModelDraft[],
  thumbFile: File | null,
  thumbUrl: string | null,
): boolean {
  if (thumbFile && !thumbUrl) {
    return false;
  }
  if (files.some((file) => file.uploadStatus !== "uploaded" || !file.url)) {
    return false;
  }
  return models.every(
    (modelDraft) =>
      (!modelDraft.thumbFile || Boolean(modelDraft.thumbUrl)) &&
      modelDraft.files.every(
        (file) => file.uploadStatus === "uploaded" && Boolean(file.url),
      ),
  );
}

/* ------------------------------------------------------------------ *
 * Draft persistence (localStorage)
 * ------------------------------------------------------------------ */

export type StoredFile = {
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
  extReference?: ExtFileReference;
};

export type StoredModel = {
  id: string;
  name: string;
  description: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  files: StoredFile[];
  metrics: MetricDraft[];
  program?: ProgramDraft | null;
};

export type StoredDraft = {
  version: number;
  entryId: string;
  extExperimentId?: string | null;
  name: string;
  description: string;
  thumbUrl: string | null;
  thumbPreview?: string;
  thumbUploadStatus: UploadStatus;
  files: StoredFile[];
  models: StoredModel[];
};

export function isPersistable(file: ParsedFile): boolean {
  return file.uploadStatus === "uploaded" && Boolean(file.url);
}

export function httpOnly(value: string | null | undefined): string | undefined {
  return value && /^https?:/i.test(value) ? value : undefined;
}

export function fileToDraft(file: ParsedFile): StoredFile {
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
    extReference: file.extReference,
  };
}

export function fileFromDraft(file: StoredFile): ParsedFile {
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
    extReference: file.extReference,
  };
}

export function modelToDraft(modelDraft: ModelDraft): StoredModel {
  return {
    id: modelDraft.id,
    name: modelDraft.name,
    description: modelDraft.description,
    thumbUrl: modelDraft.thumbUrl,
    thumbPreview: httpOnly(modelDraft.thumbPreview),
    files: modelDraft.files.filter(isPersistable).map(fileToDraft),
    metrics: modelDraft.metrics,
    program: modelDraft.program,
  };
}

export function modelFromDraft(modelDraft: StoredModel): ModelDraft {
  return {
    id: modelDraft.id,
    name: modelDraft.name,
    description: modelDraft.description,
    thumbFileId: crypto.randomUUID(),
    thumbFile: null,
    thumbPreview: modelDraft.thumbPreview ?? null,
    thumbUrl: modelDraft.thumbUrl,
    thumbProgress: modelDraft.thumbUrl ? 1 : 0,
    thumbUploadStatus: modelDraft.thumbUrl ? "uploaded" : "idle",
    thumbUploadError: null,
    files: modelDraft.files.map(fileFromDraft).map(normalizeModelLevel),
    metrics: modelDraft.metrics,
    program: modelDraft.program ?? null,
  };
}

export function draftHasContent(draft: StoredDraft): boolean {
  return (
    draft.name.trim().length > 0 ||
    draft.description.trim().length > 0 ||
    Boolean(draft.extExperimentId) ||
    Boolean(draft.thumbUrl) ||
    draft.files.length > 0 ||
    draft.models.length > 0
  );
}

export function readStoredDraft(): StoredDraft | null {
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
    parsed.models = Array.isArray(parsed.models)
      ? parsed.models
      : [];
    return parsed;
  } catch {
    return null;
  }
}

export function writeStoredDraft(draft: StoredDraft): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(DRAFT_STORAGE_KEY, JSON.stringify(draft));
  } catch {
    // Storage full or unavailable — drafts are best effort.
  }
}

export function clearStoredDraft(): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.removeItem(DRAFT_STORAGE_KEY);
  } catch {
    // ignore
  }
}

export function detectType(filename: string): string {
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

export function parseFasta(text: string): Record<string, unknown> {
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

export function formatSize(size: number): string {
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

function SearchIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-4-4" />
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
