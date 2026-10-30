"use client";

import { useId } from "react";

import ChipListInput from "./ChipListInput";
import CompoundListsEditor from "./CompoundListsEditor";
import { ProgressBar } from "./FilesEditor";
import { Field, FormSection } from "./FormFields";
import { CompoundListsReadOnly, ReadOnlyText } from "./ReadOnlyFields";
import { MODEL_COMPOUND_FIELDS, type FieldId } from "./fields";
import { uploadStatusText } from "./helpers";
import { UploadIcon } from "./icons";
import { inlineMessages, type FieldMessage } from "./messages";
import {
  archiveLockLabel,
  type ArchiveLock,
  type ModelArchiveField,
} from "./revisionChanges";
import { MODEL_PURPOSES, MODEL_TYPES } from "./types";
import type { CompoundsDraft, UploadStatus } from "./types";
import styles from "./form.module.css";

/** The values of a model's Overview section, on the New and Edit forms. */
export type ModelOverviewValues = {
  title: string;
  purpose: string;
  modelType: string;
  details: string;
  authors: string[];
  affiliation: string;
  compounds: CompoundsDraft;
};

/** Read-only parts of the Overview section. */
export type ModelOverviewLocks = {
  fields: Partial<Record<ModelArchiveField, ArchiveLock>>;
  /** Deposited and Reference are set by the system, never chosen. */
  purpose: boolean;
  modelType: boolean;
};

const NO_LOCKS: ModelOverviewLocks = { fields: {}, purpose: false, modelType: false };

/**
 * A model's Overview section: title, purpose, type, description,
 * attribution and composition. A locked value is shown read-only with its
 * source badge.
 */
export default function ModelOverviewFields({
  values,
  onPatch,
  thumbnail,
  onThumbnail,
  compoundsSource = null,
  compoundsFromFile = false,
  locks = NO_LOCKS,
  messages,
}: {
  values: ModelOverviewValues;
  onPatch: (patch: Partial<ModelOverviewValues>) => void;
  thumbnail: {
    preview: string | null;
    status: UploadStatus;
    progress: number;
    error: string | null;
  };
  onThumbnail: (file: File) => void;
  /** Where the composition was read from, such as the coordinate file. */
  compoundsSource?: string | null;
  /** The model file is in our bucket, so the server reads its compounds and
   *  the form asks for none. */
  compoundsFromFile?: boolean;
  locks?: ModelOverviewLocks;
  messages: readonly FieldMessage[];
}) {
  // Field ids come from useId: a draft id minted with crypto.randomUUID()
  // differs between the server and the client render, which left labels
  // pointing at ids no input carried.
  const fieldId = useId();
  const inline = (field: FieldId, item: string | null = null) =>
    inlineMessages(messages, field, item);
  const badge = (field: ModelArchiveField) => {
    const lock = locks.fields[field];
    return lock ? archiveLockLabel(lock) : null;
  };
  const compoundsLock = locks.fields.ligands;

  return (
    <FormSection id="overview">
      {/* Same identity block as the entry form. The thumbnail is last in
          the DOM and moved by CSS so tabbing still starts at the title. */}
      <div className={styles.identityRow}>
        <div className={styles.identityMain}>
          <Field
            id="model.title"
            htmlFor={`${fieldId}-title`}
            messages={inline("model.title")}
          >
            <input
              id={`${fieldId}-title`}
              className={styles.input}
              value={values.title}
              onChange={(event) => onPatch({ title: event.target.value })}
              placeholder="e.g. Refined structure (REFMAC)"
              autoComplete="off"
              data-1p-ignore
              data-lpignore="true"
              data-form-type="other"
            />
          </Field>

          {/* What the run was for and what came out are properties of the
              model, like its title — not a separate stage of the form. */}
          <div className={styles.identityChoices}>
            <Field
              id="model.purpose"
              htmlFor={locks.purpose ? undefined : `${fieldId}-purpose`}
              messages={inline("model.purpose")}
            >
              {locks.purpose ? (
                <ReadOnlyText value={values.purpose} />
              ) : (
                <select
                  id={`${fieldId}-purpose`}
                  className={styles.select}
                  value={values.purpose}
                  onChange={(event) => onPatch({ purpose: event.target.value })}
                >
                  <option value="">—</option>
                  {MODEL_PURPOSES.map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              )}
            </Field>
            <Field
              id="model.type"
              htmlFor={locks.modelType ? undefined : `${fieldId}-type`}
              messages={inline("model.type")}
            >
              {locks.modelType ? (
                <ReadOnlyText value={values.modelType} />
              ) : (
                <select
                  id={`${fieldId}-type`}
                  className={styles.select}
                  value={values.modelType}
                  onChange={(event) => onPatch({ modelType: event.target.value })}
                >
                  <option value="">—</option>
                  {MODEL_TYPES.map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              )}
            </Field>
          </div>
        </div>

        <div className={styles.identityThumb}>
          <Field id="model.thumbnail" messages={inline("model.thumbnail")}>
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
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  if (file) {
                    onThumbnail(file);
                  }
                }}
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

      <Field
        id="model.details"
        htmlFor={locks.fields.details ? undefined : `${fieldId}-details`}
        messages={inline("model.details")}
        source={badge("details")}
      >
        {locks.fields.details ? (
          <ReadOnlyText value={values.details} />
        ) : (
          <textarea
            id={`${fieldId}-details`}
            className={styles.textarea}
            rows={3}
            value={values.details}
            onChange={(event) => onPatch({ details: event.target.value })}
            placeholder="e.g. Multiconformer model built with qFit against the deposited data, then refined in phenix."
          />
        )}
      </Field>

      <div className={styles.fieldGrid}>
        <Field
          id="model.authors"
          htmlFor={locks.fields.authors ? undefined : `${fieldId}-authors`}
          messages={inline("model.authors")}
          source={badge("authors")}
        >
          {locks.fields.authors ? (
            <ReadOnlyText value={values.authors.join("; ")} />
          ) : (
            <ChipListInput
              id={`${fieldId}-authors`}
              values={values.authors}
              onChange={(authors) => onPatch({ authors })}
              placeholder="e.g. Hendrickson, W.A. — press Enter"
              separators={/[;\n]/}
            />
          )}
        </Field>
        <Field
          id="model.affiliation"
          htmlFor={locks.fields.affiliation ? undefined : `${fieldId}-affiliation`}
          messages={inline("model.affiliation")}
          source={badge("affiliation")}
        >
          {locks.fields.affiliation ? (
            <ReadOnlyText value={values.affiliation} />
          ) : (
            <input
              id={`${fieldId}-affiliation`}
              className={styles.input}
              value={values.affiliation}
              onChange={(event) => onPatch({ affiliation: event.target.value })}
              placeholder="e.g. University of California, San Francisco"
              autoComplete="off"
            />
          )}
        </Field>
      </div>

      {compoundsFromFile ? (
        <p className={styles.sectionNote}>
          The compounds this model contains are read from its file after you save, and appear on
          the model&apos;s page a few minutes later. The entry&apos;s Ligands and Cofactors lists
          say which of them matter.
        </p>
      ) : compoundsLock ? (
        <CompoundListsReadOnly
          compounds={values.compounds}
          fields={MODEL_COMPOUND_FIELDS}
          source={archiveLockLabel(compoundsLock)}
        />
      ) : (
        <CompoundListsEditor
          compounds={values.compounds}
          onChange={(compounds) => onPatch({ compounds })}
          fields={MODEL_COMPOUND_FIELDS}
          messages={messages}
          source={compoundsSource}
        />
      )}
    </FormSection>
  );
}
