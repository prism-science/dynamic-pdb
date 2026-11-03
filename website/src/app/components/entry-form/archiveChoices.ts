import type { Entry, EntryRevisionChange } from "@/lib/api/entries";

import type { ArchiveAutofill } from "./autofill";
import { crystallographyRequest, optionalNumber } from "./crystals";
import { FIELDS, type FieldId, type SectionId } from "./fields";
import {
  ENTRY_ARCHIVE_POINTERS,
  archiveLockLabel,
  entryPdbId,
  type ArchiveLock,
  type EntryArchiveField,
  type EntryLocks,
} from "./revisionChanges";
import type { CrystalDraft, EntryMetadataDraft } from "./types";

// The depositor's choice between the PDB archive's value of an entry field and
// their own (researcher answer Q4 B: the archive sync never changes a
// depositor's value). A field the archive filled is read-only until the
// depositor takes it over with "Use my own value"; the archive value then
// stays beside theirs, and "Use the PDB value" goes back to it. The server
// records the source on its own: a value the form sends that differs from the
// archive's is the depositor's, so no request carries the choice itself.

/** The archive fields in the order the form shows them. */
export const ENTRY_ARCHIVE_FIELDS: readonly EntryArchiveField[] = [
  "title",
  "details",
  "method",
  "resolution",
  "space_group",
  "crystallography",
];

export function isEntryArchiveField(value: unknown): value is EntryArchiveField {
  return ENTRY_ARCHIVE_FIELDS.some((field) => field === value);
}

/** own with field taken over (true) or handed back (false), in form order. */
export function withOwnField(
  own: readonly EntryArchiveField[],
  field: EntryArchiveField,
  isOwn: boolean,
): EntryArchiveField[] {
  return ENTRY_ARCHIVE_FIELDS.filter((each) => (each === field ? isOwn : own.includes(each)));
}

/** The archive fields that are the depositor's own, from a saved draft. A
 *  value this form does not know is dropped. */
export function ownFieldsFromStored(value: unknown): EntryArchiveField[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return ENTRY_ARCHIVE_FIELDS.filter((field) => value.includes(field));
}

/** One value per archive field, in the form's draft shape. */
export type ArchiveFieldValues = {
  title: string;
  details: string;
  method: string;
  resolution: string;
  space_group: string;
  crystallography: CrystalDraft[];
};

/** The form's title and metadata, the way the forms hold them. */
export type EntryFormValues = { title: string; metadata: EntryMetadataDraft };

export function archiveFieldValues(form: EntryFormValues): ArchiveFieldValues {
  return {
    title: form.title,
    details: form.metadata.details,
    method: form.metadata.method,
    resolution: form.metadata.resolution,
    space_group: form.metadata.spaceGroup,
    crystallography: form.metadata.crystals,
  };
}

/** The archive's values as the New entry form fills them; a value the
 *  archive does not have is blank. */
export function archiveValuesFromAutofill(autofill: ArchiveAutofill | null): ArchiveFieldValues {
  return {
    title: autofill?.title ?? "",
    details: autofill?.metadata.details ?? "",
    method: autofill?.metadata.method ?? "",
    resolution: autofill?.metadata.resolution ?? "",
    space_group: autofill?.metadata.spaceGroup ?? "",
    crystallography: autofill?.metadata.crystals ?? [],
  };
}

/** The form with field set to source's value of it. */
export function withValueFrom<Field extends EntryArchiveField>(
  form: EntryFormValues,
  field: Field,
  source: ArchiveFieldValues,
): EntryFormValues {
  const values = archiveFieldValues(form);
  values[field] = source[field];
  return {
    title: values.title,
    metadata: {
      ...form.metadata,
      details: values.details,
      method: values.method,
      resolution: values.resolution,
      spaceGroup: values.space_group,
      crystals: values.crystallography,
    },
  };
}

/** Whether a and b hold the same value of field, the way the request would
 *  carry it: text without surrounding spaces, numbers as numbers, crystals
 *  by their values rather than the form's keys. */
export function sameArchiveValue(
  field: EntryArchiveField,
  a: ArchiveFieldValues,
  b: ArchiveFieldValues,
): boolean {
  switch (field) {
    case "title":
      return a.title.trim() === b.title.trim();
    case "details":
      return a.details.trim() === b.details.trim();
    case "method":
      return a.method.trim() === b.method.trim();
    case "space_group":
      return a.space_group.trim() === b.space_group.trim();
    case "resolution":
      return optionalNumber(a.resolution) === optionalNumber(b.resolution);
    case "crystallography":
      return (
        JSON.stringify(crystallographyRequest(a.crystallography) ?? null) ===
        JSON.stringify(crystallographyRequest(b.crystallography) ?? null)
      );
    default:
      throw new Error(`unknown entry archive field: ${String(field)}`);
  }
}

/** Whether values holds no value of field. */
export function isBlankArchiveValue(field: EntryArchiveField, values: ArchiveFieldValues): boolean {
  switch (field) {
    case "title":
      return !values.title.trim();
    case "details":
      return !values.details.trim();
    case "method":
      return !values.method.trim();
    case "space_group":
      return !values.space_group.trim();
    case "resolution":
      return !values.resolution.trim();
    case "crystallography":
      return values.crystallography.length === 0;
    default:
      throw new Error(`unknown entry archive field: ${String(field)}`);
  }
}

/**
 * New entry, "Use my own value": the depositor's typed value when there is
 * one, otherwise a copy of the archive's to start from.
 */
export function takeOver(
  form: EntryFormValues,
  field: EntryArchiveField,
  archive: ArchiveFieldValues,
): EntryFormValues {
  return isBlankArchiveValue(field, archiveFieldValues(form))
    ? withValueFrom(form, field, archive)
    : form;
}

/**
 * The New entry form's autofill without the fields the depositor took over:
 * those show, check and send the depositor's value instead.
 */
export function withoutOwnFields(
  autofill: ArchiveAutofill,
  own: readonly EntryArchiveField[],
): ArchiveAutofill {
  const kept: ArchiveAutofill = {
    locks: { ...autofill.locks },
    title: autofill.title,
    metadata: { ...autofill.metadata },
    request: { ...autofill.request },
  };
  for (const field of own) {
    // The request keys are the field names.
    delete kept.locks[field];
    delete kept.request[field];
    switch (field) {
      case "title":
        kept.title = null;
        break;
      case "details":
        delete kept.metadata.details;
        break;
      case "method":
        delete kept.metadata.method;
        break;
      case "resolution":
        delete kept.metadata.resolution;
        break;
      case "space_group":
        delete kept.metadata.spaceGroup;
        break;
      case "crystallography":
        delete kept.metadata.crystals;
        break;
      default:
        throw new Error(`unknown entry archive field: ${String(field)}`);
    }
  }
  return kept;
}

/** How the form shows one field the archive filled. */
export type ArchiveFieldState = {
  lock: ArchiveLock;
  /** The depositor took the field over: it is an input again. */
  own: boolean;
  /** New entry: the depositor typed a different value before the lookup. */
  conflict: boolean;
  /** The depositor's own value equals the archive's, so it stays the
   *  archive's and the weekly update keeps it current. */
  sameAsArchive: boolean;
};

export type ArchiveFieldStates = Partial<Record<EntryArchiveField, ArchiveFieldState>>;

/** Everything the form sections need to show the archive fields. */
export type ArchiveFieldsView = {
  states: ArchiveFieldStates;
  /** The archive's values, shown beside a value the depositor took over. */
  archive: ArchiveFieldValues;
  /** The depositor's values; on New entry, what they typed. */
  form: ArchiveFieldValues;
};

/**
 * The state of each field in locks, the fields the archive filled.
 * showConflicts is for New entry, where a value typed before the lookup
 * stays under the archive's: when it differs, the form says so.
 */
export function archiveFieldsView(
  locks: EntryLocks,
  archive: ArchiveFieldValues,
  form: ArchiveFieldValues,
  own: readonly EntryArchiveField[],
  showConflicts: boolean,
): ArchiveFieldsView {
  const states: ArchiveFieldStates = {};
  for (const field of ENTRY_ARCHIVE_FIELDS) {
    const lock = locks[field];
    if (!lock) {
      continue;
    }
    const isOwn = own.includes(field);
    const same = sameArchiveValue(field, form, archive);
    states[field] = {
      lock,
      own: isOwn,
      conflict:
        showConflicts && !isOwn && !same && !isBlankArchiveValue(field, form),
      sameAsArchive: isOwn && same,
    };
  }
  return { states, archive, form };
}

/** The archive fields shown read-only: filled by the archive and not taken over. */
export function lockedArchiveFields(states: ArchiveFieldStates): EntryArchiveField[] {
  return ENTRY_ARCHIVE_FIELDS.filter((field) => {
    const state = states[field];
    return state !== undefined && !state.own;
  });
}

/** The form fields that show each archive field. */
const ARCHIVE_FORM_FIELD_IDS: Record<EntryArchiveField, FieldId[]> = {
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

/** The form section that shows field, whose API messages a choice clears. */
export function archiveFieldSection(field: EntryArchiveField): SectionId {
  return FIELDS[ARCHIVE_FORM_FIELD_IDS[field][0]].section;
}

/** The form fields of the read-only archive fields: their messages cannot be
 *  acted on, so they do not block a submit. A field taken over is an input
 *  again, and its messages block as any other's. */
export function lockedFieldIds(states: ArchiveFieldStates): Set<FieldId> {
  return new Set(
    lockedArchiveFields(states).flatMap((field) => ARCHIVE_FORM_FIELD_IDS[field]),
  );
}

/** The badge of a field the archive filled. */
export function archiveFieldBadge(state: ArchiveFieldState): string {
  return state.own ? "your value" : archiveLockLabel(state.lock);
}

/** "PDB 7L0B", naming the archive value beside the depositor's own. */
export function archiveValueSource(lock: ArchiveLock): string {
  return lock.accession ? `PDB ${lock.accession}` : "The PDB";
}

export const SAME_AS_ARCHIVE_HINT =
  "Same as the PDB value, so it stays the PDB's and the weekly update keeps it current.";

// ---------------------------------------------------- sync-written fields --

/** The entry fields the archive sync writes (EntryArchiveFields in
 *  backend/internal/models/field_sources.go), by request key. */
export const ENTRY_SYNCED_POINTERS = {
  ...ENTRY_ARCHIVE_POINTERS,
  ligands: "/metadata/ligands",
  cofactors: "/metadata/cofactors",
} as const;

export type EntrySyncedField = keyof typeof ENTRY_SYNCED_POINTERS;

/** In the order the form shows them: the composition lists sit under Overview. */
const ENTRY_SYNCED_FIELDS: readonly EntrySyncedField[] = [
  "title",
  "details",
  "ligands",
  "cofactors",
  "method",
  "resolution",
  "space_group",
  "crystallography",
];

const SYNCED_FIELD_LABELS: Record<EntrySyncedField, string> = {
  title: "Title",
  details: "Description",
  method: "Method",
  resolution: "Resolution",
  space_group: "Space group",
  crystallography: "Crystals",
  ligands: "Ligands",
  cofactors: "Cofactors",
};

export const DEPOSITOR_BADGE = "set by the depositor";

/**
 * Edit entry: the badges of the sync-written fields the archive does not lock.
 * A field the depositor set says so. Ligands and cofactors stay editable
 * whatever their source, so they also carry the archive's badge when the sync
 * wrote them. A field without a source has no badge.
 */
export function entrySourceBadges(entry: Entry): Partial<Record<EntrySyncedField, string>> {
  const badges: Partial<Record<EntrySyncedField, string>> = {};
  for (const field of ENTRY_SYNCED_FIELDS) {
    const source = entry.field_sources?.[ENTRY_SYNCED_POINTERS[field]];
    if (!source) {
      continue;
    }
    switch (source.source) {
      case "depositor":
        badges[field] = DEPOSITOR_BADGE;
        break;
      case "rcsb":
        if (field === "ligands" || field === "cofactors") {
          badges[field] = archiveLockLabel({
            accession: source.accession ?? entryPdbId(entry),
            release: source.release ?? null,
          });
        }
        break;
      default:
        throw new Error(`unknown field source: ${String(source.source)}`);
    }
  }
  return badges;
}

/**
 * Edit entry: the fields this change takes from the weekly archive update. A
 * sync-written field the change sets becomes the depositor's, unless it
 * already was. An entry without a PDB ID is never synced.
 */
export function editTakenOverFields(entry: Entry, change: EntryRevisionChange): EntrySyncedField[] {
  if (!entryPdbId(entry)) {
    return [];
  }
  return ENTRY_SYNCED_FIELDS.filter(
    (field) =>
      field in change &&
      entry.field_sources?.[ENTRY_SYNCED_POINTERS[field]]?.source !== "depositor",
  );
}

/**
 * New entry: the fields the depositor took over with a value of their own. A
 * value equal to the archive's stays the archive's, and a blank one is not
 * sent, so the update can still fill it.
 */
export function newTakenOverFields(view: ArchiveFieldsView): EntryArchiveField[] {
  return ENTRY_ARCHIVE_FIELDS.filter((field) => {
    const state = view.states[field];
    return (
      state !== undefined &&
      state.own &&
      !state.sameAsArchive &&
      !isBlankArchiveValue(field, view.form)
    );
  });
}

/** The line above the submit button; null when no field is taken over. */
export function takenOverNotice(fields: readonly EntrySyncedField[]): string | null {
  if (fields.length === 0) {
    return null;
  }
  const labels = fields.map((field) => SYNCED_FIELD_LABELS[field]).join(", ");
  return `The weekly PDB update will no longer change these fields, because you set them yourself: ${labels}.`;
}
