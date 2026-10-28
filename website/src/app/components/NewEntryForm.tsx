"use client";

import { useEffect, useState, type FormEvent } from "react";

import type { ArchiveEntryProposal } from "@/lib/api/entries";
import {
  createEntryAction,
  lookupArchiveEntryAction,
} from "@/app/entries/new/actions";

import DraftRestorePrompt, {
  draftSummary,
} from "./entry-form/DraftRestorePrompt";
import EntryFormSections, { lockedFieldIds } from "./entry-form/EntryFormSections";
import FilesEditor from "./entry-form/FilesEditor";
import { Field, SubmitBlockers } from "./entry-form/FormFields";
import type { ArchiveLookupState } from "./entry-form/PolymerEntitiesEditor";
import { FIELDS, type FieldId, type SectionId } from "./entry-form/fields";
import {
  blockerLines,
  inlineMessages,
  type FieldMessage,
} from "./entry-form/messages";
import {
  PDB_ID_PATTERN,
  polymerEntityDraftsFromArchive,
} from "./entry-form/polymerEntities";
import {
  archiveAutofill,
  autofilledMetadata,
  autofilledTitle,
  withAutofilledRequest,
} from "./entry-form/autofill";
import { entryFormMessages } from "./entry-form/validation";
import { DRAFT_VERSION, emptyEntryMetadataDraft } from "./entry-form/types";
import type { EntryMetadataDraft, ParsedFile } from "./entry-form/types";
import {
  assignCanonicalArtifactTypes,
  buildCreateEntryInput,
  parseFile,
  parseUrlFile,
  uploadParsedFileNow,
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
import { useThumbnailUpload } from "./entry-form/useThumbnailUpload";
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
  const thumbnail = useThumbnailUpload({ entryId, modelId: null });
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
  // The entry values the last finished lookup proposed, for the PDB ID it read.
  const [archiveEntry, setArchiveEntry] = useState<{
    pdbId: string;
    proposal: ArchiveEntryProposal;
  } | null>(null);

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
      thumbUrl: thumbnail.url,
      thumbPreview: httpOnly(thumbnail.preview),
      thumbUploadStatus: thumbnail.url ? "uploaded" : "idle",
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
    thumbnail.url,
    thumbnail.preview,
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
      lookupArchiveEntryAction(pdbId)
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
          setArchiveEntry({ pdbId, proposal: result.entry });
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
    thumbnail.restore(draft.thumbUrl, draft.thumbPreview ?? null);
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
    thumbnail.reset();
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

  const hasPendingUploads =
    thumbnail.status === "uploading" ||
    files.some((file) => file.uploadStatus === "uploading");

  const hasFailedUploads =
    thumbnail.status === "failed" ||
    files.some((file) => file.uploadStatus === "failed");

  // The archive's values fill the fields it has, read-only, once the lookup
  // for the current PDB ID has finished. What the depositor typed stays
  // underneath, and shows again when the PDB ID is cleared or changed.
  const autofill =
    lookup.status === "ready" && archiveEntry?.pdbId === lookup.pdbId
      ? archiveAutofill(lookup.pdbId, archiveEntry.proposal)
      : null;
  const locks = autofill?.locks ?? {};
  const shownTitle = autofilledTitle(title, autofill);
  const shownMetadata = autofilledMetadata(metadata, autofill);

  // Entities from an earlier PDB ID, or from a lookup that has not finished,
  // are neither checked nor sent.
  const submittedMetadata: EntryMetadataDraft =
    lookup.status === "ready"
      ? shownMetadata
      : { ...shownMetadata, polymerEntities: [] };
  // A filled-in value cannot be fixed here, so its messages never block.
  const locked = lockedFieldIds(locks);
  const validationMessages = entryFormMessages(shownTitle, submittedMetadata).filter(
    (message) => !locked.has(message.field),
  );
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
      if (!uploadsReady(files, thumbnail.file, thumbnail.url)) {
        setError("Wait until all files are uploaded.");
        setSubmitting(false);
        return;
      }

      const input = withAutofilledRequest(
        buildCreateEntryInput(shownTitle, submittedMetadata, thumbnail.url, files),
        autofill,
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

      <EntryFormSections
        title={shownTitle}
        onTitle={(next) => {
          setTitle(next);
          dropApiMessages("overview");
        }}
        thumbnail={thumbnail}
        metadata={shownMetadata}
        onEdit={editMetadata}
        onPdb={(pdb) =>
          // Typed entity values belong to the PDB entry they were typed
          // for; a different ID starts the block over.
          editMetadata("overview", (current) =>
            current.pdb === pdb
              ? current
              : { ...current, pdb, polymerEntities: [] },
          )
        }
        lookup={lookup}
        onRetryLookup={() => setLookupAttempt((attempt) => attempt + 1)}
        locks={locks}
        messages={messages}
        data={
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
        }
      />

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
