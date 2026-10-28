"use client";

import { useState, type FormEvent } from "react";

import type { Entity, Entry } from "@/lib/api/entries";
import { updateEntryAction } from "@/app/entries/[entryId]/edit/actions";

import ChangeSummaryField from "./entry-form/ChangeSummaryField";
import EntryFormSections, {
  lockedFieldIds,
  type EditMetadata,
} from "./entry-form/EntryFormSections";
import { SubmitBlockers } from "./entry-form/FormFields";
import type { ArchiveLookupState } from "./entry-form/PolymerEntitiesEditor";
import { FilesReadOnly } from "./entry-form/ReadOnlyFields";
import { FIELDS, type SectionId } from "./entry-form/fields";
import { blockerLines, type FieldMessage } from "./entry-form/messages";
import {
  buildEntryRevisionChange,
  entryArchiveLocks,
  entryDraftFromEntry,
  entryPdbId,
  hasChanges,
} from "./entry-form/revisionChanges";
import type { EntryMetadataDraft } from "./entry-form/types";
import { useThumbnailUpload } from "./entry-form/useThumbnailUpload";
import { entryFormMessages } from "./entry-form/validation";
import styles from "./entry-form/form.module.css";

/**
 * Edits a published entry. Saving sends only the fields the depositor
 * changed, as a revision a reviewer approves. Values the PDB archive keeps
 * up to date are read-only, and files cannot change yet.
 */
export default function EditEntryForm({
  entry,
  files,
}: {
  entry: Entry;
  /** The entry's own files, shown read-only. */
  files: Entity[];
}) {
  const [initial] = useState(() => entryDraftFromEntry(entry));
  const [title, setTitle] = useState(initial.title);
  const [metadata, setMetadata] = useState<EntryMetadataDraft>(initial.metadata);
  const thumbnail = useThumbnailUpload(
    { entryId: entry.id, modelId: null },
    initial.thumbUrl,
  );
  const [changeSummary, setChangeSummary] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [apiMessages, setApiMessages] = useState<FieldMessage[]>([]);

  const locks = entryArchiveLocks(entry);
  const storedPdb = entryPdbId(entry);
  // The entities shown are the entry's stored ones, read from its PDB entry.
  const lookup: ArchiveLookupState = storedPdb
    ? { status: "ready", pdbId: storedPdb }
    : { status: "idle" };

  const dropApiMessages = (section: SectionId) =>
    setApiMessages((current) =>
      current.filter((message) => FIELDS[message.field].section !== section),
    );
  const editMetadata: EditMetadata = (section, update) => {
    setMetadata(update);
    dropApiMessages(section);
  };

  const change = buildEntryRevisionChange(entry, initial, {
    title,
    thumbUrl: thumbnail.url,
    metadata,
  });
  const changed = hasChanges(change);

  // A read-only value cannot be fixed here, so its messages never block.
  const locked = lockedFieldIds(locks);
  const validationMessages = entryFormMessages(title, metadata).filter(
    (message) => !locked.has(message.field),
  );
  const messages = [...validationMessages, ...apiMessages];
  const blockers = [
    ...blockerLines(validationMessages),
    ...(thumbnail.status === "uploading"
      ? ["Preview image: wait until it has finished uploading"]
      : []),
    ...(thumbnail.status === "failed"
      ? ["Preview image: choose it again, or keep the current one"]
      : []),
  ];
  const canSubmit = changed && blockers.length === 0 && !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);
    setApiMessages([]);
    const summary = changeSummary.trim();
    const result = await updateEntryAction(entry.id, {
      ...change,
      ...(summary ? { change_summary: summary } : {}),
    });
    // On success the action redirects to the entry page.
    if (result) {
      setError(result.error);
      setApiMessages(result.fieldErrors);
      setSubmitting(false);
    }
  };

  const entryHref = `/entries/${encodeURIComponent(entry.id)}`;

  return (
    <form className={styles.form} onSubmit={submit} noValidate>
      <EntryFormSections
        title={title}
        onTitle={(next) => {
          setTitle(next);
          dropApiMessages("overview");
        }}
        thumbnail={thumbnail}
        metadata={metadata}
        onEdit={editMetadata}
        onPdb={(pdb) => editMetadata("overview", (current) => ({ ...current, pdb }))}
        pdbReadOnly={storedPdb !== null}
        lookup={lookup}
        onRetryLookup={() => undefined}
        locks={locks}
        messages={messages}
        data={
          <FilesReadOnly
            files={files}
            note="Adding files to an existing entry comes later."
          />
        }
      />

      <ChangeSummaryField value={changeSummary} onChange={setChangeSummary} />

      {error ? <p className={styles.error}>{error}</p> : null}
      {submitting ? null : <SubmitBlockers action="save" lines={blockers} />}

      <div className={styles.actions}>
        <div className={styles.actionsRight}>
          <a className={styles.secondary} href={entryHref}>
            Cancel
          </a>
          <button type="submit" className={styles.primary} disabled={!canSubmit}>
            {submitting ? "Saving…" : changed ? "Save change" : "Change at least one field"}
          </button>
        </div>
      </div>
    </form>
  );
}
