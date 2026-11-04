"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";

import {
  createEntryAction,
  lookupArchiveEntryAction,
} from "@/app/entries/new/actions";

import DraftRestorePrompt, {
  draftSummary,
} from "./entry-form/DraftRestorePrompt";
import EntryFormSections from "./entry-form/EntryFormSections";
import FileReadStatus from "./entry-form/FileReadStatus";
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
import {
  archiveFieldSection,
  archiveFieldValues,
  archiveFieldsView,
  archiveValuesFromAutofill,
  lockedFieldIds,
  newTakenOverFields,
  takeOver,
  takenOverNotice,
  withOwnField,
  withoutOwnFields,
} from "./entry-form/archiveChoices";
import type { EntryArchiveField } from "./entry-form/revisionChanges";
import {
  entryFileBadges,
  entryFileMessages,
  entryValueSourcesRequest,
  pdbSuggestion,
  withEntryFileValues,
  type EntryFileFill,
  type ReadEntryFile,
} from "./entry-form/entryFileFills";
import {
  lookupReviewMessages,
  type ArchiveLookupReview,
} from "./entry-form/lookupReview";
import { uploadedStructure, useFileExtraction } from "./entry-form/useFileExtraction";
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
  // What the last finished lookup proposed, for the PDB ID it read: the
  // entry values the form fills in, and the conflicts and warnings it shows.
  const [archiveEntry, setArchiveEntry] = useState<{
    pdbId: string;
    review: ArchiveLookupReview;
  } | null>(null);
  // The archive fields the depositor took over: their typed values are shown
  // and sent instead of the archive's. A refetch never changes this list.
  const [ownFields, setOwnFields] = useState<EntryArchiveField[]>([]);
  // Values filled from uploaded mmCIF files the server read, and what each
  // read file said, for the notes beside the fields.
  const [fileFills, setFileFills] = useState<EntryFileFill[]>([]);
  const [readFiles, setReadFiles] = useState<ReadEntryFile[]>([]);
  const metadataRef = useRef(metadata);
  metadataRef.current = metadata;

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
      ownFields,
      fileFills,
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
    ownFields,
    fileFills,
  ]);

  // The polymer entities come from the PDB entry: without their
  // label_entity_id the API cannot tell which one a construct belongs to.
  const pdbId = metadata.pdb.trim().toUpperCase();
  const pdbIsComplete = PDB_ID_PATTERN.test(pdbId);

  // An uploaded mmCIF is read on the server; its values fill what the
  // depositor left empty, and stay the file's while unchanged.
  const extraction = useFileExtraction((file, extracted) => {
    const pdb = metadataRef.current.pdb.trim().toUpperCase();
    const { fill } = withEntryFileValues(metadataRef.current, file, extracted, pdb);
    setMetadata((current) => withEntryFileValues(current, file, extracted, pdb).metadata);
    setFileFills((current) => [...current.filter((item) => item.fileId !== file.id), fill]);
    setReadFiles((current) => [
      ...current.filter((item) => item.fileId !== file.id),
      { fileId: file.id, fileName: file.name, extracted },
    ]);
  });
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
          setArchiveEntry({
            pdbId,
            review: {
              entry: result.entry,
              polymerEntities: result.polymerEntities,
              warnings: result.warnings,
            },
          });
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
    setOwnFields(draft.ownFields);
    setFileFills(draft.fileFills ?? []);
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
    setOwnFields([]);
    setFileFills([]);
    setReadFiles([]);
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
      void uploadParsedFileNow(file, { entryId, modelId: null }, patchEntryFile).then((url) => {
        const structure = url ? uploadedStructure({ ...file, url, uploadStatus: "uploaded" }) : null;
        if (structure) {
          void extraction.read(structure);
        }
      });
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
  // underneath, and shows again when the PDB ID is cleared or changed, or
  // when they take the field over with "Use my own value".
  const finishedLookup =
    lookup.status === "ready" && archiveEntry?.pdbId === lookup.pdbId
      ? archiveEntry
      : null;
  const archiveFill = finishedLookup
    ? archiveAutofill(finishedLookup.pdbId, finishedLookup.review.entry)
    : null;
  // Advice only: the depositor can take a field over, not fix the archive.
  const reviewMessages = finishedLookup
    ? lookupReviewMessages(finishedLookup.pdbId, finishedLookup.review)
    : [];
  const autofill = archiveFill ? withoutOwnFields(archiveFill, ownFields) : null;
  const archiveFields = archiveFieldsView(
    archiveFill?.locks ?? {},
    archiveValuesFromAutofill(archiveFill),
    archiveFieldValues({ title, metadata }),
    ownFields,
    true,
  );
  const shownTitle = autofilledTitle(title, autofill);
  const shownMetadata = autofilledMetadata(metadata, autofill);

  const chooseOwn = (field: EntryArchiveField, own: boolean) => {
    if (own) {
      const taken = takeOver({ title, metadata }, field, archiveFields.archive);
      setTitle(taken.title);
      setMetadata(taken.metadata);
    }
    setOwnFields((current) => withOwnField(current, field, own));
    dropApiMessages(archiveFieldSection(field));
  };
  const notice = takenOverNotice(newTakenOverFields(archiveFields));

  // Entities from an earlier PDB ID, or from a lookup that has not finished,
  // are neither checked nor sent.
  const submittedMetadata: EntryMetadataDraft =
    lookup.status === "ready"
      ? shownMetadata
      : { ...shownMetadata, polymerEntities: [] };
  // A filled-in value cannot be fixed here, so its messages never block.
  const locked = lockedFieldIds(archiveFields.states);
  const validationMessages = entryFormMessages(shownTitle, submittedMetadata).filter(
    (message) => !locked.has(message.field),
  );
  const fileMessages = entryFileMessages(readFiles, archiveFill, pdbId);
  const messages = [...validationMessages, ...apiMessages, ...reviewMessages, ...fileMessages];
  // The request as it would be sent now, to tell which file values still hold.
  const currentInput = withAutofilledRequest(
    buildCreateEntryInput(shownTitle, submittedMetadata, thumbnail.url, files),
    autofill,
  );
  const fileBadges = entryFileBadges(currentInput, fileFills, files);
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

      const built = withAutofilledRequest(
        buildCreateEntryInput(shownTitle, submittedMetadata, thumbnail.url, files),
        autofill,
      );
      const input = {
        ...built,
        value_sources: entryValueSourcesRequest(built, fileFills, files),
      };

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
        archiveFields={archiveFields}
        onOwn={chooseOwn}
        badges={fileBadges}
        pdbSuggestion={pdbSuggestion(readFiles, pdbId)}
        messages={messages}
        data={
          <Field id="entry.files" messages={inline("entry.files")}>
            <FilesEditor
              files={files}
              lockLevel
              fileMessages={(fileId) => inline("entry.files", fileId)}
              onAdd={addEntryFiles}
              onAddUrl={addEntryUrl}
              onRemove={(id) => {
                extraction.forget(id);
                setReadFiles((current) => current.filter((item) => item.fileId !== id));
                editFiles((p) => p.filter((f) => f.id !== id));
              }}
              onLevel={(id, level) =>
                editFiles((p) => p.map((f) => (f.id === id ? { ...f, level } : f)))
              }
              onPatch={(id, patch) =>
                editFiles((p) =>
                  p.map((f) => (f.id === id ? { ...f, ...patch } : f)),
                )
              }
            />
            <FileReadStatus states={extraction.states} onRetry={(file) => void extraction.read(file)} />
          </Field>
        }
      />

      {notice ? <p className={styles.takenOverNotice}>{notice}</p> : null}
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
