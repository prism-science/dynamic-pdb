import type { Entity } from "@/lib/api/entries";

import { COMPOUND_LIST_NAMES } from "./compounds";
import { crystalIds } from "./crystals";
import { Field, ReadOnlyValue } from "./FormFields";
import type { CompoundListFields } from "./fields";
import type { CompoundList, CompoundsDraft, CrystalDraft } from "./types";
import styles from "./form.module.css";

// Values an edit form shows but does not let the depositor change: values
// the PDB archive keeps up to date, and files, which an edit cannot change
// yet (T2-G).

/** A read-only text value; an empty one says so. */
export function ReadOnlyText({ value }: { value: string }) {
  return <ReadOnlyValue>{value.trim() || "Not given"}</ReadOnlyValue>;
}

/** Each crystal's conditions as one line, under the ids the request uses. */
export function CrystalsReadOnly({ crystals }: { crystals: CrystalDraft[] }) {
  if (crystals.length === 0) {
    return <ReadOnlyValue>No crystals given</ReadOnlyValue>;
  }
  const ids = crystalIds(crystals);
  return (
    <ul className={styles.fileList}>
      {crystals.map((crystal, index) => (
        <li key={crystal.key}>
          <ReadOnlyValue>{crystalLine(crystal, ids[index].crystalId)}</ReadOnlyValue>
        </li>
      ))}
    </ul>
  );
}

function crystalLine(crystal: CrystalDraft, crystalId: string): string {
  const parts = [
    crystal.ph.trim() ? `pH ${crystal.ph.trim()}` : null,
    crystal.growthTemperature.trim()
      ? `grown at ${crystal.growthTemperature.trim()} K`
      : null,
    ...crystal.diffractions.map((diffraction) =>
      diffraction.temperature.trim()
        ? `data collected at ${diffraction.temperature.trim()} K`
        : "data collected, temperature not given",
    ),
  ].filter((part): part is string => part !== null);
  return `Crystal ${crystalId}: ${parts.length > 0 ? parts.join(", ") : "no conditions given"}`;
}

/** Ligands and cofactors as the archive lists them. */
export function CompoundListsReadOnly({
  compounds,
  fields,
  source,
}: {
  compounds: CompoundsDraft;
  fields: CompoundListFields;
  source: string;
}) {
  return (
    <div className={styles.fieldGrid}>
      {COMPOUND_LIST_NAMES.map((list) => (
        <Field key={list} id={fields[list]} source={source}>
          <ReadOnlyValue>{compoundListText(compounds[list])}</ReadOnlyValue>
        </Field>
      ))}
    </div>
  );
}

function compoundListText(list: CompoundList): string {
  switch (list.mode) {
    case "unknown":
      return "Not known";
    case "none":
      return "None";
    case "list":
      return list.items.map((item) => item.id).join(", ");
    default:
      throw new Error(`unknown compound list mode: ${String(list.mode)}`);
  }
}

/** The files or runs already stored, which an edit cannot change yet. */
export function FilesReadOnly({
  files,
  note,
  empty = "No files",
}: {
  files: Entity[];
  note: string;
  empty?: string;
}) {
  return (
    <>
      <p className={styles.sectionNote}>{note}</p>
      {files.length > 0 ? (
        <ul className={styles.fileList}>
          {files.map((file) => (
            <li key={file.id}>
              <ReadOnlyValue>
                {file.name}
                {file.level ? ` · ${file.level}` : ""}
              </ReadOnlyValue>
            </li>
          ))}
        </ul>
      ) : (
        <ReadOnlyValue>{empty}</ReadOnlyValue>
      )}
    </>
  );
}
