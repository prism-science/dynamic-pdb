"use client";

import { useId } from "react";

import { Field, FieldMessages } from "./FormFields";
import { PlusIcon } from "./icons";
import { inlineMessages, type FieldMessage } from "./messages";
import { FIELDS, type FieldId } from "./fields";
import {
  DATA_TYPE_SUGGESTIONS,
  LINK_EVIDENCE_OPTIONS,
  MAX_RELATED_DATASETS,
  RELATION_OPTIONS,
  REPOSITORY_SUGGESTIONS,
  emptyRelatedDataset,
  relatedDatasetItem,
} from "./relatedDatasets";
import type { RelatedDatasetDraft } from "./types";
import styles from "./form.module.css";

const ROW_FIELDS: FieldId[] = [
  "relatedDataset.doi",
  "relatedDataset.accession",
  "relatedDataset.landingUrl",
  "relatedDataset.dataType",
  "relatedDataset.repository",
  "relatedDataset.relation",
  "relatedDataset.linkEvidence",
  "relatedDataset.details",
];

/**
 * Datasets kept in other repositories, one card each, linked by DOI or by the
 * repository's accession. The
 * request carries no id for a dataset, so an API error about one comes back
 * without an item and is shown above the cards; the form's own checks, which
 * repeat the API's rules, name the card.
 */
export default function RelatedDatasetsEditor({
  datasets,
  onChange,
  messages,
}: {
  datasets: RelatedDatasetDraft[];
  onChange: (next: RelatedDatasetDraft[]) => void;
  messages: readonly FieldMessage[];
}) {
  const listId = useId();
  const unplaced = [
    ...inlineMessages(messages, "entry.relatedDatasets"),
    ...ROW_FIELDS.flatMap((field) => inlineMessages(messages, field)),
  ];

  const patch = (key: string, next: Partial<RelatedDatasetDraft>) =>
    onChange(
      datasets.map((dataset) =>
        dataset.key === key ? { ...dataset, ...next } : dataset,
      ),
    );

  return (
    <div className={styles.formField}>
      <div className={styles.fieldHead}>
        <span className={styles.fieldLabel}>{FIELDS["entry.relatedDatasets"].label}</span>
      </div>
      <p className={styles.fieldHelp}>{FIELDS["entry.relatedDatasets"].help}</p>
      <FieldMessages messages={unplaced} />
      <datalist id={`${listId}-data-types`}>
        {DATA_TYPE_SUGGESTIONS.map((value) => (
          <option key={value} value={value} />
        ))}
      </datalist>
      <datalist id={`${listId}-repositories`}>
        {REPOSITORY_SUGGESTIONS.map((value) => (
          <option key={value} value={value} />
        ))}
      </datalist>
      <div className={styles.filesEditor}>
        {datasets.map((dataset, index) => (
          <RelatedDatasetCard
            key={dataset.key}
            dataset={dataset}
            item={relatedDatasetItem(index)}
            dataTypeList={`${listId}-data-types`}
            repositoryList={`${listId}-repositories`}
            messages={messages}
            onPatch={(next) => patch(dataset.key, next)}
            onRemove={() =>
              onChange(datasets.filter((item) => item.key !== dataset.key))
            }
          />
        ))}
        {datasets.length < MAX_RELATED_DATASETS ? (
          <button
            type="button"
            className={styles.fileDrop}
            onClick={() => onChange([...datasets, emptyRelatedDataset()])}
          >
            <PlusIcon />
            Add related dataset
          </button>
        ) : null}
      </div>
    </div>
  );
}

function RelatedDatasetCard({
  dataset,
  item,
  dataTypeList,
  repositoryList,
  messages,
  onPatch,
  onRemove,
}: {
  dataset: RelatedDatasetDraft;
  item: string;
  dataTypeList: string;
  repositoryList: string;
  messages: readonly FieldMessage[];
  onPatch: (next: Partial<RelatedDatasetDraft>) => void;
  onRemove: () => void;
}) {
  const fieldId = useId();

  return (
    <div className={styles.metricCard}>
      <div className={styles.metricTop}>
        <span className={styles.programTag}>Dataset {item}</span>
        <button
          type="button"
          className={styles.remove}
          onClick={onRemove}
          aria-label={`Remove dataset ${item}`}
        >
          ×
        </button>
      </div>

      <div className={styles.fieldGrid}>
        <Field
          id="relatedDataset.doi"
          htmlFor={`${fieldId}-doi`}
          messages={inlineMessages(messages, "relatedDataset.doi", item)}
        >
          <input
            id={`${fieldId}-doi`}
            className={styles.input}
            value={dataset.doi}
            onChange={(event) => onPatch({ doi: event.target.value })}
            placeholder="e.g. 10.15785/SBGRID/1303"
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
        <Field
          id="relatedDataset.accession"
          htmlFor={`${fieldId}-accession`}
          messages={inlineMessages(messages, "relatedDataset.accession", item)}
        >
          <input
            id={`${fieldId}-accession`}
            className={styles.input}
            value={dataset.accession}
            onChange={(event) => onPatch({ accession: event.target.value })}
            placeholder="e.g. EMPIAR-10028"
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
        <Field
          id="relatedDataset.dataType"
          htmlFor={`${fieldId}-data-type`}
          messages={inlineMessages(messages, "relatedDataset.dataType", item)}
        >
          <input
            id={`${fieldId}-data-type`}
            className={styles.input}
            list={dataTypeList}
            value={dataset.dataType}
            onChange={(event) => onPatch({ dataType: event.target.value })}
            placeholder="e.g. diffraction image data"
            autoComplete="off"
          />
        </Field>
        <Field
          id="relatedDataset.repository"
          htmlFor={`${fieldId}-repository`}
          messages={inlineMessages(messages, "relatedDataset.repository", item)}
        >
          <input
            id={`${fieldId}-repository`}
            className={styles.input}
            list={repositoryList}
            value={dataset.repository}
            onChange={(event) => onPatch({ repository: event.target.value })}
            placeholder="e.g. SBGrid Data Bank"
            autoComplete="off"
          />
        </Field>
      </div>
      <Field
        id="relatedDataset.landingUrl"
        htmlFor={`${fieldId}-landing-url`}
        messages={inlineMessages(messages, "relatedDataset.landingUrl", item)}
      >
        <input
          id={`${fieldId}-landing-url`}
          className={styles.input}
          type="url"
          inputMode="url"
          value={dataset.landingUrl}
          onChange={(event) => onPatch({ landingUrl: event.target.value })}
          placeholder="e.g. https://www.ebi.ac.uk/empiar/EMPIAR-10028/"
          autoComplete="off"
          spellCheck={false}
        />
      </Field>
      <div className={styles.fieldGrid}>
        <Field
          id="relatedDataset.relation"
          htmlFor={`${fieldId}-relation`}
          messages={inlineMessages(messages, "relatedDataset.relation", item)}
        >
          <ChoiceSelect
            id={`${fieldId}-relation`}
            value={dataset.relation}
            options={RELATION_OPTIONS}
            onChange={(relation) => onPatch({ relation })}
          />
        </Field>
        <Field
          id="relatedDataset.linkEvidence"
          htmlFor={`${fieldId}-link-evidence`}
          messages={inlineMessages(messages, "relatedDataset.linkEvidence", item)}
        >
          <ChoiceSelect
            id={`${fieldId}-link-evidence`}
            value={dataset.linkEvidence}
            options={LINK_EVIDENCE_OPTIONS}
            onChange={(linkEvidence) => onPatch({ linkEvidence })}
          />
        </Field>
      </div>
      <Field
        id="relatedDataset.details"
        htmlFor={`${fieldId}-details`}
        messages={inlineMessages(messages, "relatedDataset.details", item)}
      >
        <textarea
          id={`${fieldId}-details`}
          className={styles.textarea}
          rows={2}
          value={dataset.details}
          onChange={(event) => onPatch({ details: event.target.value })}
          placeholder="e.g. Collected from similar crystals in October 2025."
        />
      </Field>
    </div>
  );
}

/** A closed list with "Not stated" first. A stored value the list lacks, from a
 *  newer API, stays selectable as itself so an edit does not drop it. */
function ChoiceSelect({
  id,
  value,
  options,
  onChange,
}: {
  id: string;
  value: string;
  options: readonly { value: string; label: string }[];
  onChange: (next: string) => void;
}) {
  const unlisted = value !== "" && !options.some((option) => option.value === value);
  return (
    <select
      id={id}
      className={styles.select}
      value={value}
      onChange={(event) => onChange(event.target.value)}
    >
      <option value="">Not stated</option>
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
      {unlisted ? <option value={value}>{value}</option> : null}
    </select>
  );
}
