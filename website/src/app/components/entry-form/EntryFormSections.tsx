"use client";

import type { ChangeEvent, ReactNode } from "react";

import ArchiveChoice, { typedText } from "./ArchiveChoice";
import CompoundListsEditor from "./CompoundListsEditor";
import CrystalsEditor from "./CrystalsEditor";
import { ProgressBar } from "./FilesEditor";
import { Field, FormSection } from "./FormFields";
import PolymerEntitiesEditor, { type ArchiveLookupState } from "./PolymerEntitiesEditor";
import { CrystalsReadOnly, ReadOnlyText } from "./ReadOnlyFields";
import RelatedDatasetsEditor from "./RelatedDatasetsEditor";
import { UploadIcon } from "./icons";
import {
  archiveFieldBadge,
  type ArchiveFieldsView,
  type EntrySyncedField,
} from "./archiveChoices";
import { ENTRY_COMPOUND_FIELDS, FIELDS, type FieldId, type SectionId } from "./fields";
import { sanitizeNumeric, uploadStatusText } from "./helpers";
import { inlineMessages, type FieldMessage } from "./messages";
import type { EntryArchiveField } from "./revisionChanges";
import { METHODS, type EntryMetadataDraft } from "./types";
import type { ThumbnailUpload } from "./useThumbnailUpload";
import styles from "./form.module.css";

export type EditMetadata = (
  section: SectionId,
  update: (current: EntryMetadataDraft) => EntryMetadataDraft,
) => void;

/**
 * The sections of the New entry and Edit entry forms. A field the PDB archive
 * filled is shown read-only with its source badge until the depositor takes
 * it over (ArchiveChoice); every other field is an input.
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
  archiveFields,
  onOwn,
  badges = {},
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
  /** The fields the archive filled, and whether the depositor took each over. */
  archiveFields: ArchiveFieldsView;
  /** "Use my own value" (true) and "Use the PDB value" (false). */
  onOwn: (field: EntryArchiveField, own: boolean) => void;
  /** Badges of the fields the archive did not fill, such as "set by the
   *  depositor" on the Edit form. */
  badges?: Partial<Record<EntrySyncedField, string>>;
  messages: readonly FieldMessage[];
  /** The Data section's files: an editor on the New form, a list on Edit.
   *  The related datasets editor follows them on both. */
  data: ReactNode;
}) {
  const inline = (field: FieldId, item: string | null = null) =>
    inlineMessages(messages, field, item);
  const { states, archive, form } = archiveFields;
  const locked = (field: EntryArchiveField) => {
    const state = states[field];
    return state !== undefined && !state.own;
  };
  const badge = (field: EntryArchiveField) => {
    const state = states[field];
    return state ? archiveFieldBadge(state) : (badges[field] ?? null);
  };
  const choice = (
    field: EntryArchiveField,
    fieldLabel: string,
    archiveValue: ReactNode,
    typedValue: ReactNode,
  ) => (
    <ArchiveChoice
      state={states[field]}
      fieldLabel={fieldLabel}
      archiveValue={archiveValue}
      typedValue={typedValue}
      onOwn={(own) => onOwn(field, own)}
    />
  );

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
              htmlFor={locked("title") ? undefined : "entry-title"}
              messages={inline("entry.title")}
              source={badge("title")}
            >
              {locked("title") ? (
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
              {choice(
                "title",
                FIELDS["entry.title"].label,
                archive.title,
                typedText(form.title),
              )}
            </Field>
            <Field
              id="entry.details"
              htmlFor={locked("details") ? undefined : "entry-details"}
              messages={inline("entry.details")}
              source={badge("details")}
            >
              {locked("details") ? (
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
              {choice(
                "details",
                FIELDS["entry.details"].label,
                archive.details,
                typedText(form.details),
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
          listSources={{ ligands: badges.ligands, cofactors: badges.cofactors }}
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
            htmlFor={locked("method") ? undefined : "entry-method"}
            messages={inline("entry.method")}
            source={badge("method")}
          >
            {locked("method") ? (
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
            {choice(
              "method",
              FIELDS["entry.method"].label,
              archive.method,
              typedText(form.method),
            )}
          </Field>
          <Field
            id="entry.resolution"
            htmlFor={locked("resolution") ? undefined : "entry-resolution"}
            messages={inline("entry.resolution")}
            source={badge("resolution")}
          >
            {locked("resolution") ? (
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
            {choice(
              "resolution",
              FIELDS["entry.resolution"].label,
              archive.resolution,
              typedText(form.resolution),
            )}
          </Field>
          <Field
            id="entry.spaceGroup"
            htmlFor={locked("space_group") ? undefined : "entry-space-group"}
            messages={inline("entry.spaceGroup")}
            source={badge("space_group")}
          >
            {locked("space_group") ? (
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
            {choice(
              "space_group",
              FIELDS["entry.spaceGroup"].label,
              archive.space_group,
              typedText(form.space_group),
            )}
          </Field>
        </div>

        {badge("crystallography") ? (
          <div className={styles.fieldHead}>
            <span className={styles.subLabel}>Crystals</span>
            <span className={styles.sourceBadge}>{badge("crystallography")}</span>
          </div>
        ) : (
          <span className={styles.subLabel}>Crystals</span>
        )}
        <p className={styles.fieldHelp}>{FIELDS["crystal.block"].help}</p>
        {locked("crystallography") ? (
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
        {choice(
          "crystallography",
          "Crystals",
          <CrystalsReadOnly crystals={archive.crystallography} />,
          "You typed different crystals.",
        )}
      </FormSection>

      <FormSection id="data">
        {data}
        <RelatedDatasetsEditor
          datasets={metadata.relatedDatasets}
          onChange={(relatedDatasets) =>
            onEdit("data", (current) => ({ ...current, relatedDatasets }))
          }
          messages={messages}
        />
      </FormSection>
    </>
  );
}
