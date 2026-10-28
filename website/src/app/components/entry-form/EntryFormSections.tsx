"use client";

import type { ChangeEvent, ReactNode } from "react";

import CompoundListsEditor from "./CompoundListsEditor";
import CrystalsEditor from "./CrystalsEditor";
import { ProgressBar } from "./FilesEditor";
import { Field, FormSection } from "./FormFields";
import PolymerEntitiesEditor, { type ArchiveLookupState } from "./PolymerEntitiesEditor";
import { CrystalsReadOnly, ReadOnlyText } from "./ReadOnlyFields";
import { UploadIcon } from "./icons";
import { ENTRY_COMPOUND_FIELDS, FIELDS, type FieldId, type SectionId } from "./fields";
import { sanitizeNumeric, uploadStatusText } from "./helpers";
import { inlineMessages, type FieldMessage } from "./messages";
import {
  archiveLockLabel,
  type EntryArchiveField,
  type EntryLocks,
} from "./revisionChanges";
import { METHODS, type EntryMetadataDraft } from "./types";
import type { ThumbnailUpload } from "./useThumbnailUpload";
import styles from "./form.module.css";

/** The form fields that show each locked value: their messages cannot be
 *  acted on, so they do not block a submit. */
const LOCKED_FIELD_IDS: Record<EntryArchiveField, FieldId[]> = {
  title: ["entry.title"],
  details: ["entry.details"],
  method: ["entry.method"],
  resolution: ["entry.resolution"],
  space_group: ["entry.spaceGroup"],
  crystallography: [
    "crystal.block",
    "crystal.ph",
    "crystal.growthTemperature",
    "diffraction.temperature",
  ],
};

export function lockedFieldIds(locks: EntryLocks): Set<FieldId> {
  return new Set(
    (Object.keys(locks) as EntryArchiveField[]).flatMap(
      (field) => LOCKED_FIELD_IDS[field],
    ),
  );
}

export type EditMetadata = (
  section: SectionId,
  update: (current: EntryMetadataDraft) => EntryMetadataDraft,
) => void;

/**
 * The sections of the New entry and Edit entry forms. A field in locks is
 * shown read-only with its source badge; every other field is an input.
 */
export default function EntryFormSections({
  title,
  onTitle,
  thumbnail,
  metadata,
  onEdit,
  onPdb,
  pdbReadOnly = false,
  lookup,
  onRetryLookup,
  locks,
  messages,
  data,
}: {
  title: string;
  onTitle: (title: string) => void;
  thumbnail: ThumbnailUpload;
  metadata: EntryMetadataDraft;
  onEdit: EditMetadata;
  /** Called with the typed PDB ID, in capitals. */
  onPdb: (pdb: string) => void;
  /** An entry keeps the PDB ID it has. */
  pdbReadOnly?: boolean;
  lookup: ArchiveLookupState;
  onRetryLookup: () => void;
  locks: EntryLocks;
  messages: readonly FieldMessage[];
  /** The Data section's content: an editor on the New form, a list on Edit. */
  data: ReactNode;
}) {
  const inline = (field: FieldId, item: string | null = null) =>
    inlineMessages(messages, field, item);
  const badge = (field: EntryArchiveField) => {
    const lock = locks[field];
    return lock ? archiveLockLabel(lock) : null;
  };

  const onThumb = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) {
      thumbnail.choose(file);
    }
  };

  return (
    <>
      <FormSection id="overview">
        {/* Title and preview image are one identity block. The thumbnail is
            placed first visually by CSS but stays last in the DOM so the
            keyboard lands on the title field first. */}
        <div className={styles.identityRow}>
          <div className={styles.identityMain}>
            <Field
              id="entry.title"
              htmlFor={locks.title ? undefined : "entry-title"}
              messages={inline("entry.title")}
              source={badge("title")}
            >
              {locks.title ? (
                <ReadOnlyText value={title} />
              ) : (
                <input
                  id="entry-title"
                  className={styles.input}
                  value={title}
                  onChange={(event) => onTitle(event.target.value)}
                  placeholder="e.g. Hen egg-white lysozyme"
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-form-type="other"
                />
              )}
            </Field>
            <Field
              id="entry.details"
              htmlFor={locks.details ? undefined : "entry-details"}
              messages={inline("entry.details")}
              source={badge("details")}
            >
              {locks.details ? (
                <ReadOnlyText value={metadata.details} />
              ) : (
                <textarea
                  id="entry-details"
                  className={styles.textarea}
                  rows={3}
                  value={metadata.details}
                  onChange={(event) =>
                    onEdit("overview", (current) => ({
                      ...current,
                      details: event.target.value,
                    }))
                  }
                  placeholder="e.g. Room-temperature structure collected to compare with the cryo structure 5GY3."
                />
              )}
            </Field>
          </div>

          <div className={styles.identityThumb}>
            <Field id="entry.thumbnail" messages={inline("entry.thumbnail")}>
              <label className={styles.thumbDrop}>
                {thumbnail.preview ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img className={styles.thumbImg} src={thumbnail.preview} alt="" />
                ) : (
                  <span className={styles.thumbHint}>
                    <UploadIcon />
                    Choose an image
                  </span>
                )}
                {thumbnail.status === "uploading" ? (
                  <span className={styles.thumbOverlay}>
                    <ProgressBar value={thumbnail.progress} />
                  </span>
                ) : null}
                <input
                  type="file"
                  accept="image/*"
                  className={styles.hiddenInput}
                  onChange={onThumb}
                />
              </label>
              {thumbnail.status === "failed" ? (
                <span className={styles.uploadStatus} data-state="failed">
                  {uploadStatusText(thumbnail.status, thumbnail.error)}
                </span>
              ) : null}
            </Field>
          </div>
        </div>

        <div className={styles.fieldGrid}>
          <Field
            id="entry.pdb"
            htmlFor={pdbReadOnly ? undefined : "entry-pdb"}
            messages={inline("entry.pdb")}
          >
            {pdbReadOnly ? (
              <ReadOnlyText value={metadata.pdb} />
            ) : (
              <input
                id="entry-pdb"
                className={styles.input}
                value={metadata.pdb}
                onChange={(event) => onPdb(event.target.value.toUpperCase())}
                placeholder="e.g. 5GY3"
                maxLength={4}
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-form-type="other"
              />
            )}
          </Field>
        </div>

        <CompoundListsEditor
          compounds={metadata.compounds}
          onChange={(compounds) =>
            onEdit("overview", (current) => ({ ...current, compounds }))
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
            onEdit("macromolecules", (current) => ({
              ...current,
              polymerEntities,
            }))
          }
          onRetry={onRetryLookup}
          messages={messages}
        />
      </FormSection>

      <FormSection id="experiment">
        <div className={styles.fieldGrid}>
          <Field
            id="entry.method"
            htmlFor={locks.method ? undefined : "entry-method"}
            messages={inline("entry.method")}
            source={badge("method")}
          >
            {locks.method ? (
              <ReadOnlyText value={metadata.method} />
            ) : (
              <>
                <input
                  id="entry-method"
                  className={styles.input}
                  list="entry-method-options"
                  value={metadata.method}
                  onChange={(event) =>
                    onEdit("experiment", (current) => ({
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
              </>
            )}
          </Field>
          <Field
            id="entry.resolution"
            htmlFor={locks.resolution ? undefined : "entry-resolution"}
            messages={inline("entry.resolution")}
            source={badge("resolution")}
          >
            {locks.resolution ? (
              <ReadOnlyText value={metadata.resolution} />
            ) : (
              <input
                id="entry-resolution"
                className={styles.input}
                inputMode="decimal"
                value={metadata.resolution}
                onChange={(event) =>
                  onEdit("experiment", (current) => ({
                    ...current,
                    resolution: sanitizeNumeric(event.target.value),
                  }))
                }
                placeholder="e.g. 1.77"
                autoComplete="off"
              />
            )}
          </Field>
          <Field
            id="entry.spaceGroup"
            htmlFor={locks.space_group ? undefined : "entry-space-group"}
            messages={inline("entry.spaceGroup")}
            source={badge("space_group")}
          >
            {locks.space_group ? (
              <ReadOnlyText value={metadata.spaceGroup} />
            ) : (
              <input
                id="entry-space-group"
                className={styles.input}
                value={metadata.spaceGroup}
                onChange={(event) =>
                  onEdit("experiment", (current) => ({
                    ...current,
                    spaceGroup: event.target.value,
                  }))
                }
                placeholder="e.g. P 21 21 21"
                autoComplete="off"
              />
            )}
          </Field>
        </div>

        {locks.crystallography ? (
          <div className={styles.fieldHead}>
            <span className={styles.subLabel}>Crystals</span>
            <span className={styles.sourceBadge}>{badge("crystallography")}</span>
          </div>
        ) : (
          <span className={styles.subLabel}>Crystals</span>
        )}
        <p className={styles.fieldHelp}>{FIELDS["crystal.block"].help}</p>
        {locks.crystallography ? (
          <CrystalsReadOnly crystals={metadata.crystals} />
        ) : (
          <CrystalsEditor
            crystals={metadata.crystals}
            onChange={(crystals) =>
              onEdit("experiment", (current) => ({ ...current, crystals }))
            }
            messages={messages}
          />
        )}
      </FormSection>

      <FormSection id="data">{data}</FormSection>
    </>
  );
}
