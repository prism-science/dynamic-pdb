"use client";

import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";

import { uploadFileToObjectStorage } from "@/lib/api/uploads";
import {
  createEntryAction,
  lookupArchivePolymerEntitiesAction,
} from "@/app/entries/new/actions";

import CompoundListsEditor from "./entry-form/CompoundListsEditor";
import CrystalsEditor from "./entry-form/CrystalsEditor";
import DraftRestorePrompt, {
  draftSummary,
} from "./entry-form/DraftRestorePrompt";
import FilesEditor, { ProgressBar } from "./entry-form/FilesEditor";
import { Field, FormSection, SubmitBlockers } from "./entry-form/FormFields";
import PolymerEntitiesEditor, {
  type ArchiveLookupState,
} from "./entry-form/PolymerEntitiesEditor";
import { UploadIcon } from "./entry-form/icons";
import {
  ENTRY_COMPOUND_FIELDS,
  FIELDS,
  type FieldId,
  type SectionId,
} from "./entry-form/fields";
import {
  blockerLines,
  inlineMessages,
  type FieldMessage,
} from "./entry-form/messages";
import {
  PDB_ID_PATTERN,
  polymerEntityDraftsFromArchive,
} from "./entry-form/polymerEntities";
import { entryFormMessages } from "./entry-form/validation";
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
  buildCreateEntryInput,
  parseFile,
  parseUrlFile,
  sanitizeNumeric,
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

// Long enough that typing a PDB ID does not start a lookup per keystroke.
const LOOKUP_DELAY_MS = 400;

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
  // Errors the API returned for the last submit, shown beside their fields
  // until the depositor edits that section.
  const [apiMessages, setApiMessages] = useState<FieldMessage[]>([]);
  const [lookup, setLookup] = useState<ArchiveLookupState>({ status: "idle" });
  const [lookupAttempt, setLookupAttempt] = useState(0);

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

  const saveDraft = () => {
    const draft: StoredDraft = {
      version: DRAFT_VERSION,
      entryId,
      metadata,
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
  };

  useEffect(() => {
    if (storageReady) {
      saveDraft();
    }
    // saveDraft reads exactly these values.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    storageReady,
    entryId,
    metadata,
    title,
    thumbUrl,
    thumbPreview,
    files,
  ]);

  // The polymer entities come from the PDB entry: without their
  // label_entity_id the API cannot tell which one a construct belongs to.
  const pdbId = metadata.pdb.trim().toUpperCase();
  const pdbIsComplete = PDB_ID_PATTERN.test(pdbId);
  useEffect(() => {
    if (!pdbIsComplete) {
      setLookup({ status: "idle" });
      return;
    }
    let cancelled = false;
    setLookup({ status: "loading", pdbId });
    const timer = window.setTimeout(() => {
      lookupArchivePolymerEntitiesAction(pdbId)
        .then((result) => {
          if (cancelled) {
            return;
          }
          if ("error" in result) {
            setLookup({ status: "failed", pdbId, message: result.error });
            return;
          }
          setMetadata((current) => ({
            ...current,
            polymerEntities: polymerEntityDraftsFromArchive(
              result.polymerEntities,
              current.polymerEntities,
            ),
          }));
          setLookup({ status: "ready", pdbId });
        })
        .catch((lookupError: unknown) => {
          if (!cancelled) {
            setLookup({
              status: "failed",
              pdbId,
              message:
                lookupError instanceof Error
                  ? lookupError.message
                  : `Could not read ${pdbId} from the PDB.`,
            });
          }
        });
    }, LOOKUP_DELAY_MS);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [pdbId, pdbIsComplete, lookupAttempt]);

  const continueDraft = () => {
    const draft = draftPrompt;
    if (!draft) {
      return;
    }
    setEntryId(draft.entryId);
    setTitle(draft.title);
    setMetadata(draft.metadata ?? emptyEntryMetadataDraft());
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
    setMetadata(emptyEntryMetadataDraft());
    activeThumbFileId.current = null;
    setThumbFile(null);
    setThumbPreview(null);
    setThumbUrl(null);
    setThumbProgress(0);
    setThumbUploadStatus("idle");
    setThumbUploadError(null);
    setFiles([]);
    setError(null);
    setApiMessages([]);
  };

  const dropApiMessages = (section: SectionId) =>
    setApiMessages((current) =>
      current.filter((message) => FIELDS[message.field].section !== section),
    );

  const editMetadata = (
    section: SectionId,
    update: (current: EntryMetadataDraft) => EntryMetadataDraft,
  ) => {
    setMetadata(update);
    dropApiMessages(section);
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

  const editFiles = (update: (current: ParsedFile[]) => ParsedFile[]) => {
    setFiles(update);
    dropApiMessages("data");
  };

  const addEntryFiles = async (list: File[]) => {
    const parsed = await Promise.all(list.map((file) => parseFile(file, "L0")));
    editFiles((current) => [
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
    editFiles((current) => [
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

  // Entities from an earlier PDB ID, or from a lookup that has not finished,
  // are neither checked nor sent.
  const submittedMetadata: EntryMetadataDraft =
    lookup.status === "ready"
      ? metadata
      : { ...metadata, polymerEntities: [] };
  const validationMessages = entryFormMessages(title, submittedMetadata);
  const messages = [...validationMessages, ...apiMessages];
  const inline = (field: FieldId, item: string | null = null) =>
    inlineMessages(messages, field, item);

  const blockers = [
    ...blockerLines(validationMessages),
    ...(hasPendingUploads
      ? ["Uploads: wait until every file has finished uploading"]
      : []),
    ...(hasFailedUploads
      ? ["Uploads: remove the files that failed to upload, or add them again"]
      : []),
    ...(lookup.status === "loading"
      ? [`PDB ID: wait until ${lookup.pdbId} has been read`]
      : []),
  ];
  const canSubmit = blockers.length === 0 && !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);
    setApiMessages([]);

    try {
      if (!uploadsReady(files, thumbFile, thumbUrl)) {
        setError("Wait until all files are uploaded.");
        setSubmitting(false);
        return;
      }

      const input = buildCreateEntryInput(
        title,
        submittedMetadata,
        thumbUrl,
        files,
      );

      // The server action redirects on success, so clear the saved draft up
      // front, and save it again if the action returns an error.
      clearStoredDraft();
      const result = await createEntryAction(input);
      if (result) {
        saveDraft();
        setError(result.error);
        setApiMessages(result.fieldErrors);
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
    <form className={styles.form} onSubmit={submit} noValidate>
      {draftPrompt ? (
        <DraftRestorePrompt
          summary={draftSummary(
            draftPrompt.title,
            draftPrompt.files.length,
            draftPrompt.metadata?.pdb.trim()
              ? `an unfinished entry for PDB ${draftPrompt.metadata.pdb.trim()}`
              : "an unfinished entry",
          )}
          onContinue={continueDraft}
          onDiscard={discardDraft}
        />
      ) : null}

      <FormSection id="overview">
        {/* Title and preview image are one identity block. The thumbnail is
            placed first visually by CSS but stays last in the DOM so the
            keyboard lands on the title field first. */}
        <div className={styles.identityRow}>
          <div className={styles.identityMain}>
            <Field
              id="entry.title"
              htmlFor="entry-title"
              messages={inline("entry.title")}
            >
              <input
                id="entry-title"
                className={styles.input}
                value={title}
                onChange={(event) => {
                  setTitle(event.target.value);
                  dropApiMessages("overview");
                }}
                placeholder="e.g. Hen egg-white lysozyme"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
            </Field>
            <Field
              id="entry.details"
              htmlFor="entry-details"
              messages={inline("entry.details")}
            >
              <textarea
                id="entry-details"
                className={styles.textarea}
                rows={3}
                value={metadata.details}
                onChange={(event) =>
                  editMetadata("overview", (current) => ({
                    ...current,
                    details: event.target.value,
                  }))
                }
                placeholder="e.g. Room-temperature structure collected to compare with the cryo structure 5GY3."
              />
            </Field>
          </div>

          <div className={styles.identityThumb}>
            <Field id="entry.thumbnail" messages={inline("entry.thumbnail")}>
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
            </Field>
          </div>
        </div>

        <div className={styles.fieldGrid}>
          <Field id="entry.pdb" htmlFor="entry-pdb" messages={inline("entry.pdb")}>
            <input
              id="entry-pdb"
              className={styles.input}
              value={metadata.pdb}
              onChange={(event) => {
                const pdb = event.target.value.toUpperCase();
                // Typed entity values belong to the PDB entry they were
                // typed for; a different ID starts the block over.
                editMetadata("overview", (current) =>
                  current.pdb === pdb
                    ? current
                    : { ...current, pdb, polymerEntities: [] },
                );
              }}
              placeholder="e.g. 5GY3"
              maxLength={4}
              autoComplete="off"
              data-1p-ignore
              data-lpignore="true"
              data-form-type="other"
            />
          </Field>
        </div>

        <CompoundListsEditor
          compounds={metadata.compounds}
          onChange={(compounds) =>
            editMetadata("overview", (current) => ({ ...current, compounds }))
          }
          fields={ENTRY_COMPOUND_FIELDS}
          messages={messages}
        />
      </FormSection>

      <FormSection id="macromolecules">
        <PolymerEntitiesEditor
          lookup={lookup}
          entities={metadata.polymerEntities}
          onChange={(polymerEntities) =>
            editMetadata("macromolecules", (current) => ({
              ...current,
              polymerEntities,
            }))
          }
          onRetry={() => setLookupAttempt((attempt) => attempt + 1)}
          messages={messages}
        />
      </FormSection>

      <FormSection id="experiment">
        <div className={styles.fieldGrid}>
          <Field
            id="entry.method"
            htmlFor="entry-method"
            messages={inline("entry.method")}
          >
            <input
              id="entry-method"
              className={styles.input}
              list="entry-method-options"
              value={metadata.method}
              onChange={(event) =>
                editMetadata("experiment", (current) => ({
                  ...current,
                  method: event.target.value,
                }))
              }
              placeholder="e.g. X-ray crystallography"
              autoComplete="off"
            />
            <datalist id="entry-method-options">
              {METHODS.map((value) => (
                <option key={value} value={value} />
              ))}
            </datalist>
          </Field>
          <Field
            id="entry.resolution"
            htmlFor="entry-resolution"
            messages={inline("entry.resolution")}
          >
            <input
              id="entry-resolution"
              className={styles.input}
              inputMode="decimal"
              value={metadata.resolution}
              onChange={(event) =>
                editMetadata("experiment", (current) => ({
                  ...current,
                  resolution: sanitizeNumeric(event.target.value),
                }))
              }
              placeholder="e.g. 1.77"
              autoComplete="off"
            />
          </Field>
          <Field
            id="entry.spaceGroup"
            htmlFor="entry-space-group"
            messages={inline("entry.spaceGroup")}
          >
            <input
              id="entry-space-group"
              className={styles.input}
              value={metadata.spaceGroup}
              onChange={(event) =>
                editMetadata("experiment", (current) => ({
                  ...current,
                  spaceGroup: event.target.value,
                }))
              }
              placeholder="e.g. P 21 21 21"
              autoComplete="off"
            />
          </Field>
        </div>

        <span className={styles.subLabel}>Crystals</span>
        <p className={styles.fieldHelp}>{FIELDS["crystal.block"].help}</p>
        <CrystalsEditor
          crystals={metadata.crystals}
          onChange={(crystals) =>
            editMetadata("experiment", (current) => ({ ...current, crystals }))
          }
          messages={messages}
        />
      </FormSection>

      <FormSection id="data">
        <Field id="entry.files" messages={inline("entry.files")}>
          <FilesEditor
            files={files}
            lockLevel
            fileMessages={(fileId) => inline("entry.files", fileId)}
            onAdd={addEntryFiles}
            onAddUrl={addEntryUrl}
            onRemove={(id) => editFiles((p) => p.filter((f) => f.id !== id))}
            onLevel={(id, level) =>
              editFiles((p) => p.map((f) => (f.id === id ? { ...f, level } : f)))
            }
            onPatch={(id, patch) =>
              editFiles((p) =>
                p.map((f) => (f.id === id ? { ...f, ...patch } : f)),
              )
            }
          />
        </Field>
      </FormSection>

      {error ? <p className={styles.error}>{error}</p> : null}
      {submitting ? null : (
        <SubmitBlockers action="create the entry" lines={blockers} />
      )}

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
