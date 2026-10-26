"use client";

import { useId } from "react";

import ChipListInput from "./ChipListInput";
import { Field, FieldMessages, ReadOnlyValue } from "./FormFields";
import { inlineMessages, type FieldMessage } from "./messages";
import {
  mutationError,
  normalizeMutation,
  organismIsEditable,
} from "./polymerEntities";
import type { PolymerEntityDraft } from "./types";
import styles from "./form.module.css";

/** Where the form stands with reading the PDB entry's polymer entities. */
export type ArchiveLookupState =
  | { status: "idle" }
  | { status: "loading"; pdbId: string }
  | { status: "ready"; pdbId: string }
  | { status: "failed"; pdbId: string; message: string };

/**
 * One block per polymer entity of the PDB entry (decision 5): the molecule
 * and its UniProt mapping are read-only, construct and mutations are always
 * typed, and the organism is typed only when the PDB names none, because the
 * API uses a depositor's organism only then.
 */
export default function PolymerEntitiesEditor({
  lookup,
  entities,
  onChange,
  onRetry,
  messages,
}: {
  lookup: ArchiveLookupState;
  entities: PolymerEntityDraft[];
  onChange: (next: PolymerEntityDraft[]) => void;
  onRetry: () => void;
  messages: readonly FieldMessage[];
}) {
  switch (lookup.status) {
    case "idle":
      return (
        <p className={styles.sectionNote}>
          The macromolecules are read from the PDB entry. Add a PDB ID under
          Overview to describe their constructs and mutations here.
        </p>
      );
    case "loading":
      return (
        <p className={styles.sectionNote}>
          Reading {lookup.pdbId} from the PDB…
        </p>
      );
    case "failed":
      return (
        <div className={styles.sectionNote} data-state="failed">
          <span>{lookup.message}</span>
          <button type="button" className={styles.secondary} onClick={onRetry}>
            Try again
          </button>
        </div>
      );
    case "ready":
      break;
    default:
      throw new Error(
        `unknown lookup status: ${(lookup as { status: string }).status}`,
      );
  }

  if (entities.length === 0) {
    return (
      <p className={styles.sectionNote}>
        The PDB lists no polymer entities for {lookup.pdbId}.
      </p>
    );
  }

  const patch = (labelEntityId: string, next: Partial<PolymerEntityDraft>) =>
    onChange(
      entities.map((entity) =>
        entity.labelEntityId === labelEntityId ? { ...entity, ...next } : entity,
      ),
    );

  return (
    <div className={styles.filesEditor}>
      {entities.map((entity) => (
        <PolymerEntityCard
          key={entity.labelEntityId}
          entity={entity}
          pdbId={lookup.pdbId}
          messages={messages}
          onPatch={(next) => patch(entity.labelEntityId, next)}
        />
      ))}
    </div>
  );
}

function PolymerEntityCard({
  entity,
  pdbId,
  messages,
  onPatch,
}: {
  entity: PolymerEntityDraft;
  pdbId: string;
  messages: readonly FieldMessage[];
  onPatch: (next: Partial<PolymerEntityDraft>) => void;
}) {
  const fieldId = useId();
  const item = entity.labelEntityId;
  const source = `from PDB ${pdbId}`;

  return (
    <div className={styles.metricCard}>
      <div className={styles.metricTop}>
        <span className={styles.programTag}>Entity {entity.labelEntityId}</span>
        <span className={styles.entityName}>
          {entity.description || "Unnamed molecule"}
        </span>
      </div>
      <FieldMessages messages={inlineMessages(messages, "entity.block", item)} />

      <div className={styles.fieldGrid}>
        <Field id="entity.uniprot" source={source}>
          <ReadOnlyValue>
            {entity.uniprotAccessions.length > 0
              ? entity.uniprotAccessions.join(", ")
              : "None mapped"}
          </ReadOnlyValue>
        </Field>

        {organismIsEditable(entity) ? (
          <Field
            id="entity.organism"
            messages={inlineMessages(messages, "entity.organism", item)}
          >
            <div className={styles.organismRow}>
              <input
                className={styles.input}
                value={entity.organismName}
                onChange={(event) => onPatch({ organismName: event.target.value })}
                placeholder="Scientific name, e.g. Homo sapiens"
                aria-label="Scientific name"
                autoComplete="off"
              />
              <input
                className={styles.input}
                inputMode="numeric"
                value={entity.organismTaxonomyId}
                onChange={(event) =>
                  onPatch({
                    organismTaxonomyId: event.target.value.replace(/[^0-9]/g, ""),
                  })
                }
                placeholder="NCBI taxon, e.g. 9606"
                aria-label="NCBI taxonomy ID"
                autoComplete="off"
              />
            </div>
          </Field>
        ) : (
          <Field
            id="entity.organism"
            source={source}
            messages={inlineMessages(messages, "entity.organism", item)}
          >
            <ReadOnlyValue>{entity.archiveOrganisms.join("; ")}</ReadOnlyValue>
          </Field>
        )}

        <Field
          id="entity.construct"
          htmlFor={`${fieldId}-construct`}
          messages={inlineMessages(messages, "entity.construct", item)}
        >
          <input
            id={`${fieldId}-construct`}
            className={styles.input}
            value={entity.construct}
            onChange={(event) => onPatch({ construct: event.target.value })}
            placeholder={
              entity.archiveConstruct ||
              "e.g. residues 1–164, N-terminal His tag"
            }
            autoComplete="off"
          />
          {entity.archiveConstruct ? (
            <span className={styles.archiveNote}>
              PDB {pdbId}: {entity.archiveConstruct}
            </span>
          ) : null}
        </Field>

        <Field
          id="entity.mutations"
          htmlFor={`${fieldId}-mutations`}
          messages={inlineMessages(messages, "entity.mutations", item)}
        >
          <ChipListInput
            id={`${fieldId}-mutations`}
            values={entity.mutations}
            onChange={(next) => onPatch({ mutations: next })}
            placeholder="e.g. C54T — press Enter"
            normalize={normalizeMutation}
            validate={mutationError}
            separators={/[,;\s]+/}
          />
          {entity.archiveMutations.length > 0 ? (
            <span className={styles.archiveNote}>
              PDB {pdbId}: {entity.archiveMutations.join(", ")}
            </span>
          ) : null}
        </Field>
      </div>
    </div>
  );
}
