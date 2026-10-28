import type { CompoundReview } from "@/lib/parse/structure";
import type {
  CreateMetricRequest,
  DepositorPolymerEntity,
  Entry,
  EntryCrystal,
  EntryCrystallography,
  EntryDiffraction,
  EntryRevisionChange,
  FieldSources,
  Metric,
  MetricKey,
  ModelDetail,
  ModelPurpose,
  ModelRevisionChange,
  PolymerEntity,
  StructureModelType,
} from "@/lib/api/entries";

import {
  compoundListRequest,
  compoundReviewRequest,
  compoundsFromParsed,
} from "./compounds";
import { optionalNumber } from "./crystals";
import { organismLabel, polymerEntitiesRequest } from "./polymerEntities";
import {
  METRIC_FIELDS,
  MODEL_PURPOSES,
  MODEL_TYPES,
  type CompoundsDraft,
  type CrystalDraft,
  type EntryMetadataDraft,
  type MetricDraft,
  type PolymerEntityDraft,
} from "./types";

// The edit forms send a revision of the active entry or model. Each builder
// turns the form's first draft and its current draft into request values
// with one function and sends only the keys whose values differ: a value the
// depositor did not touch is never sent, so it keeps its source and the
// archive sync keeps updating it. Values the form does not show (other
// external refs, crystal keys, model metadata keys) are copied from the
// active revision, never dropped.

type JSONRecord = Record<string, unknown>;

// ------------------------------------------------------------- sources --

/** Why a field is read-only on an edit form: the PDB entry its value came
 *  from, and the archive release when the sync recorded one. */
export type ArchiveLock = { accession: string | null; release: string | null };

/** The entry fields the archive sync writes, by request key, with the JSON
 *  pointer the backend records their source under. */
export const ENTRY_ARCHIVE_POINTERS = {
  title: "/title",
  details: "/metadata/details",
  resolution: "/metadata/resolution",
  method: "/metadata/method",
  space_group: "/metadata/space_group",
  crystallography: "/metadata/crystallography",
} as const;

export type EntryArchiveField = keyof typeof ENTRY_ARCHIVE_POINTERS;

/** Entry fields shown read-only, with the PDB entry their value came from. */
export type EntryLocks = Partial<Record<EntryArchiveField, ArchiveLock>>;

/** The model metadata keys the archive sync writes and the form shows. */
export const MODEL_ARCHIVE_POINTERS = {
  authors: "/metadata/authors",
  details: "/metadata/details",
  affiliation: "/metadata/affiliation",
  ligands: "/metadata/ligands",
} as const;

export type ModelArchiveField = keyof typeof MODEL_ARCHIVE_POINTERS;

/** The metrics the archive sync writes on the archive's own model
 *  (ArchiveMetricKeys in backend/internal/models/field_sources.go). */
export const ARCHIVE_METRIC_KEYS: readonly MetricKey[] = [
  "r_free",
  "r_work",
  "ramachandran_outliers",
  "clashscore",
];

export function metricPointer(key: MetricKey): string {
  return `/metrics/${key}`;
}

/**
 * The read-only rule. A field is read-only when its source is rcsb. Revisions
 * stored before sources were recorded (sources null) were imported from the
 * archive, so all their archive fields are read-only when the archive writes
 * them (archiveAccession is the PDB ID then) and editable when it does not.
 * A field with no source was never typed by a depositor and is editable.
 */
export function archiveLocks<Field extends string>(
  sources: FieldSources | undefined,
  pointers: Readonly<Record<Field, string>>,
  archiveAccession: string | null,
): Partial<Record<Field, ArchiveLock>> {
  const locks: Partial<Record<Field, ArchiveLock>> = {};
  for (const field of Object.keys(pointers) as Field[]) {
    const lock = archiveLock(sources, pointers[field], archiveAccession);
    if (lock) {
      locks[field] = lock;
    }
  }
  return locks;
}

function archiveLock(
  sources: FieldSources | undefined,
  pointer: string,
  archiveAccession: string | null,
): ArchiveLock | null {
  if (sources == null) {
    return archiveAccession
      ? { accession: archiveAccession, release: null }
      : null;
  }
  const source = sources[pointer];
  if (!source) {
    return null;
  }
  switch (source.source) {
    case "rcsb":
      return {
        accession: source.accession ?? archiveAccession,
        release: source.release ?? null,
      };
    case "depositor":
      return null;
    default:
      throw new Error(`unknown field source: ${String(source.source)}`);
  }
}

/** "from PDB 7L0B · archive 4.3", the badge beside a read-only value. */
export function archiveLockLabel(lock: ArchiveLock): string {
  const from = lock.accession ? `from PDB ${lock.accession}` : "from the PDB";
  return lock.release ? `${from} · archive ${lock.release}` : from;
}

// --------------------------------------------------------------- entry --

/** What the entry edit form holds. */
export type EntryEditDraft = {
  title: string;
  thumbUrl: string | null;
  metadata: EntryMetadataDraft;
};

export function entryPdbId(entry: Entry): string | null {
  return entry.external_refs?.pdb?.trim().toUpperCase() || null;
}

/** The read-only entry fields: the archive writes them only for an entry
 *  with a PDB ID. */
export function entryArchiveLocks(entry: Entry): EntryLocks {
  return archiveLocks(entry.field_sources, ENTRY_ARCHIVE_POINTERS, entryPdbId(entry));
}

// Stored crystals and datasets keep their ids; the draft keys say which
// stored item a draft item came from. New items get random keys.
const storedCrystalKey = (crystalId: string) => `stored-crystal:${crystalId}`;
const storedDiffractionKey = (crystalId: string, diffractionId: string) =>
  `stored-diffraction:${crystalId}:${diffractionId}`;

export function entryDraftFromEntry(entry: Entry): EntryEditDraft {
  return {
    title: entry.title ?? "",
    thumbUrl: entry.thumbnail_image_url ?? null,
    metadata: {
      details: entry.details ?? "",
      pdb: entryPdbId(entry) ?? "",
      resolution: numberText(entry.resolution),
      method: entry.method ?? "",
      spaceGroup: entry.space_group ?? "",
      compounds: compoundsFromParsed(
        { ligands: entry.ligands, cofactors: entry.cofactors },
        [],
      ),
      crystals: crystalDraftsFromStored(entry.crystallography),
      polymerEntities: polymerEntityDraftsFromEntry(
        entry.polymer_entities ?? [],
        entry.depositor_polymer_entities ?? [],
      ),
    },
  };
}

/** Stored crystals as form drafts; each keeps a key that names its stored id. */
export function crystalDraftsFromStored(
  crystallography: EntryCrystallography | undefined,
): CrystalDraft[] {
  return (crystallography?.crystals ?? []).map((crystal) => ({
    key: storedCrystalKey(crystal.id),
    ph: numberText(crystal.growth?.ph),
    growthTemperature: numberText(crystal.growth?.temperature_kelvin),
    diffractions: (crystal.diffractions ?? []).map((diffraction) => ({
      key: storedDiffractionKey(crystal.id, diffraction.id),
      temperature: numberText(diffraction.temperature_kelvin),
    })),
  }));
}

/**
 * The entry's polymer entities with what the depositor typed for each. The
 * stored entities already carry the typed values, so a typed construct,
 * mutation list or organism is shown as typed, not as the PDB's.
 */
function polymerEntityDraftsFromEntry(
  entities: PolymerEntity[],
  typed: DepositorPolymerEntity[],
): PolymerEntityDraft[] {
  return entities.flatMap((entity) => {
    const labelEntityId = entity.label_entity_id?.trim();
    if (!labelEntityId) {
      return [];
    }
    const own = typed.find((item) => item.label_entity_id === labelEntityId);
    const ownOrganism = own?.source_organisms?.[0];
    return [
      {
        labelEntityId,
        description: entity.description ?? "",
        uniprotAccessions: entity.uniprot_mappings.map((mapping) => mapping.accession),
        archiveOrganisms: ownOrganism ? [] : entity.source_organisms.map(organismLabel),
        archiveConstruct: own?.construct ? "" : (entity.construct ?? ""),
        archiveMutations: own?.mutations?.length ? [] : (entity.mutations ?? []),
        construct: own?.construct ?? "",
        mutations: own?.mutations ?? [],
        organismName: ownOrganism?.scientific_name ?? "",
        organismTaxonomyId: ownOrganism?.ncbi_taxonomy_id
          ? String(ownOrganism.ncbi_taxonomy_id)
          : "",
      },
    ];
  });
}

/** Every key the entry edit form controls, as the request would carry it.
 *  null clears a value. */
function entryRevisionValues(entry: Entry, draft: EntryEditDraft): EntryRevisionChange {
  const metadata = draft.metadata;
  return {
    title: draft.title.trim(),
    thumbnail_image_url: draft.thumbUrl,
    external_refs: externalRefsValue(entry, metadata.pdb),
    details: metadata.details.trim() || null,
    method: metadata.method.trim() || null,
    resolution: optionalNumber(metadata.resolution) ?? null,
    space_group: metadata.spaceGroup.trim() || null,
    crystallography: crystallographyValue(entry.crystallography, metadata.crystals),
    ligands: compoundListRequest(metadata.compounds.ligands) ?? null,
    cofactors: compoundListRequest(metadata.compounds.cofactors) ?? null,
    polymer_entities: polymerEntitiesValue(
      entry.depositor_polymer_entities ?? [],
      metadata.polymerEntities,
    ),
  };
}

/**
 * The change an entry edit sends: only the keys the depositor changed, and
 * never a read-only one. An empty object means nothing changed.
 */
export function buildEntryRevisionChange(
  entry: Entry,
  initial: EntryEditDraft,
  current: EntryEditDraft,
): EntryRevisionChange {
  const locks = entryArchiveLocks(entry);
  const change = changedValues(
    entryRevisionValues(entry, initial),
    entryRevisionValues(entry, current),
  );
  for (const field of Object.keys(locks) as EntryArchiveField[]) {
    delete change[field];
  }
  return change;
}

/** The PDB ID joins the entry's other references. Once an entry has one, it
 *  is never changed here. */
function externalRefsValue(
  entry: Entry,
  draftPdb: string,
): Record<string, string> | null {
  const refs = { ...(entry.external_refs ?? {}) };
  const pdb = entryPdbId(entry) ?? draftPdb.trim().toUpperCase();
  if (pdb) {
    refs.pdb = pdb;
  } else {
    delete refs.pdb;
  }
  return Object.keys(refs).length > 0 ? refs : null;
}

/**
 * The crystals as the request carries them. A stored crystal or dataset
 * keeps its id and every key the form does not show; only the values the
 * form edits are replaced. New ones get the next free number.
 */
function crystallographyValue(
  stored: EntryCrystallography | undefined,
  drafts: CrystalDraft[],
): EntryCrystallography | null {
  const storedCrystals = stored?.crystals ?? [];
  const crystalsByKey = new Map(
    storedCrystals.map((crystal) => [storedCrystalKey(crystal.id), crystal]),
  );
  const diffractionsByKey = new Map(
    storedCrystals.flatMap((crystal) =>
      (crystal.diffractions ?? []).map(
        (diffraction) =>
          [storedDiffractionKey(crystal.id, diffraction.id), diffraction] as const,
      ),
    ),
  );
  const nextCrystalId = freeIds(storedCrystals.map((crystal) => crystal.id));
  const nextDiffractionId = freeIds(
    [...diffractionsByKey.values()].map((diffraction) => diffraction.id),
  );

  const crystals = drafts.map((draft): EntryCrystal => {
    const original = crystalsByKey.get(draft.key);
    const growth: JSONRecord = { ...(original?.growth ?? {}) };
    setOptionalNumber(growth, "ph", draft.ph);
    setOptionalNumber(growth, "temperature_kelvin", draft.growthTemperature);
    const diffractions = draft.diffractions.map((diffractionDraft): EntryDiffraction => {
      const storedDiffraction = diffractionsByKey.get(diffractionDraft.key);
      const diffraction: JSONRecord = {
        ...(storedDiffraction ?? {}),
        id: storedDiffraction?.id ?? nextDiffractionId(),
      };
      setOptionalNumber(diffraction, "temperature_kelvin", diffractionDraft.temperature);
      return diffraction as EntryDiffraction;
    });
    const crystal: JSONRecord = { ...(original ?? {}), id: original?.id ?? nextCrystalId() };
    setOrDelete(crystal, "growth", Object.keys(growth).length > 0 ? growth : undefined);
    setOrDelete(crystal, "diffractions", diffractions.length > 0 ? diffractions : undefined);
    return crystal as EntryCrystal;
  });

  const others = { ...(stored ?? {}) };
  delete others.crystals;
  if (crystals.length === 0 && Object.keys(others).length === 0) {
    return null;
  }
  return { ...others, crystals };
}

/** Numbers the stored items do not use yet, from 1 up. */
function freeIds(used: string[]): () => string {
  const taken = new Set(used);
  let next = 1;
  return () => {
    while (taken.has(String(next))) {
      next++;
    }
    taken.add(String(next));
    return String(next);
  };
}

/** The depositor's typed values: those of the entities the form shows, plus
 *  the stored ones of entities it does not show. Never null: the API
 *  rejects a null list. */
function polymerEntitiesValue(
  stored: DepositorPolymerEntity[],
  drafts: PolymerEntityDraft[],
): DepositorPolymerEntity[] {
  const shown = new Set(drafts.map((draft) => draft.labelEntityId));
  const fromForm = new Map(
    (polymerEntitiesRequest(drafts) ?? []).map((entity) => [entity.label_entity_id, entity]),
  );
  return [
    ...stored.filter((entity) => !shown.has(entity.label_entity_id)),
    ...drafts.flatMap((draft) => {
      const typed = fromForm.get(draft.labelEntityId);
      // The form shows one organism; any others typed through the API stay.
      const hiddenOrganisms =
        stored.find((entity) => entity.label_entity_id === draft.labelEntityId)
          ?.source_organisms?.slice(1) ?? [];
      if (hiddenOrganisms.length === 0) {
        return typed ? [typed] : [];
      }
      return [
        {
          ...(typed ?? { label_entity_id: draft.labelEntityId }),
          source_organisms: [...(typed?.source_organisms ?? []), ...hiddenOrganisms],
        },
      ];
    }),
  ];
}

// --------------------------------------------------------------- model --

/** What the model edit form holds. Files and runs are not here: the form
 *  shows them read-only. */
export type ModelEditDraft = {
  title: string;
  thumbUrl: string | null;
  purpose: string;
  modelType: string;
  details: string;
  authors: string[];
  affiliation: string;
  compounds: CompoundsDraft;
  metrics: MetricDraft[];
};

/** Read-only parts of a model edit form. */
export type ModelLocks = {
  fields: Partial<Record<ModelArchiveField, ArchiveLock>>;
  metrics: Partial<Record<MetricKey, ArchiveLock>>;
  /** Deposited and Reference are set by the system, never by a depositor. */
  purpose: boolean;
  modelType: boolean;
};

export function modelLocks(model: ModelDetail): ModelLocks {
  const metadata = model.metadata ?? {};
  // The sync writes only the archive's own model, and reads it from the PDB
  // ID the model itself names.
  const archiveAccession =
    metadata.model_type === "Deposited" ? modelPdbId(metadata.external_refs) : null;
  const metricPointers = Object.fromEntries(
    ARCHIVE_METRIC_KEYS.map((key) => [key, metricPointer(key)]),
  ) as Record<MetricKey, string>;
  return {
    fields: archiveLocks(model.field_sources, MODEL_ARCHIVE_POINTERS, archiveAccession),
    metrics: archiveLocks(model.field_sources, metricPointers, archiveAccession),
    purpose: isSystemChoice(metadata.purpose, MODEL_PURPOSES),
    modelType: isSystemChoice(metadata.model_type, MODEL_TYPES),
  };
}

function modelPdbId(externalRefs: unknown): string | null {
  const pdb = asRecord(externalRefs).pdb;
  return typeof pdb === "string" ? pdb.trim().toUpperCase() || null : null;
}

function isSystemChoice(
  value: unknown,
  depositorChoices: readonly (ModelPurpose | StructureModelType)[],
): boolean {
  return (
    typeof value === "string" &&
    value !== "" &&
    !(depositorChoices as readonly string[]).includes(value)
  );
}

export function modelDraftFromModel(model: ModelDetail): ModelEditDraft {
  const metadata = model.metadata ?? {};
  return {
    title: model.title ?? "",
    thumbUrl: model.thumbnail_image_url ?? null,
    purpose: stringValue(metadata.purpose),
    modelType: stringValue(metadata.model_type),
    details: stringValue(metadata.details),
    authors: stringList(metadata.authors) ?? [],
    affiliation: stringValue(metadata.affiliation),
    compounds: compoundsFromParsed(
      { ligands: stringList(metadata.ligands), cofactors: stringList(metadata.cofactors) },
      compoundReviews(metadata.extensions),
    ),
    metrics: metricGroups(model.metrics ?? []).map((group) => ({
      id: group.id,
      values: Object.fromEntries(
        group.metrics.map((metric) => [metric.key, String(metric.value)]),
      ),
      softwareName: group.softwareName ?? "",
      softwareVersion: group.softwareVersion ?? "",
    })),
  };
}

/** The metadata values the model edit form controls, by metadata key. */
function modelMetadataValues(draft: ModelEditDraft): Record<string, unknown> {
  const authors = draft.authors.map((author) => author.trim()).filter(Boolean);
  const review = compoundReviewRequest(draft.compounds);
  return {
    purpose: draft.purpose.trim() || undefined,
    model_type: draft.modelType.trim() || undefined,
    details: draft.details.trim() || undefined,
    authors: authors.length > 0 ? authors : undefined,
    affiliation: draft.affiliation.trim() || undefined,
    ligands: compoundListRequest(draft.compounds.ligands),
    cofactors: compoundListRequest(draft.compounds.cofactors),
    compound_review: review.length > 0 ? review : undefined,
  };
}

/**
 * The change a model edit sends: only what the depositor changed, and never
 * a read-only value. metadata replaces the whole stored object, so it starts
 * from the active revision's and replaces only the edited keys. Metrics
 * replace the whole set: they are sent only when one changed, with fresh
 * ids, and each keeps the run and software it came with.
 */
export function buildModelRevisionChange(
  model: ModelDetail,
  initial: ModelEditDraft,
  current: ModelEditDraft,
  newId: () => string = () => crypto.randomUUID(),
): ModelRevisionChange {
  const locks = modelLocks(model);
  const change: ModelRevisionChange = {};

  const title = current.title.trim();
  if (title !== initial.title.trim()) {
    change.title = title;
  }
  if (current.thumbUrl !== initial.thumbUrl) {
    change.thumbnail_image_url = current.thumbUrl;
  }

  const editedMetadata = changedValues(
    modelMetadataValues(initial),
    modelMetadataValues(current),
  );
  for (const field of Object.keys(locks.fields) as ModelArchiveField[]) {
    delete editedMetadata[field];
    if (field === "ligands") {
      // Ligands and cofactors are one list split in two: both or neither.
      delete editedMetadata.cofactors;
      delete editedMetadata.compound_review;
    }
  }
  if (locks.purpose) {
    delete editedMetadata.purpose;
  }
  if (locks.modelType) {
    delete editedMetadata.model_type;
  }
  if (Object.keys(editedMetadata).length > 0) {
    change.metadata = patchedMetadata(model.metadata ?? {}, editedMetadata);
  }

  const before = metricValues(model, initial.metrics, locks.metrics);
  const after = metricValues(model, current.metrics, locks.metrics);
  if (!sameJSON(before, after)) {
    change.metrics = after.map((metric) => ({ ...metric, id: newId() }));
  }
  return change;
}

function patchedMetadata(
  stored: JSONRecord,
  edited: Record<string, unknown>,
): JSONRecord {
  // residue_data never comes inside metadata; the backend keeps it.
  const metadata: JSONRecord = { ...stored };
  for (const [key, value] of Object.entries(edited)) {
    if (key === "compound_review") {
      const extensions: JSONRecord = { ...asRecord(metadata.extensions) };
      setOrDelete(extensions, "compound_review", value);
      setOrDelete(
        metadata,
        "extensions",
        Object.keys(extensions).length > 0 ? extensions : undefined,
      );
      continue;
    }
    setOrDelete(metadata, key, value);
  }
  return metadata;
}

type MetricGroup = {
  id: string;
  softwareName: string | null;
  softwareVersion: string | null;
  runId: string | null;
  metrics: Metric[];
};

/** The stored metrics as the form shows them: one set per program run. */
function metricGroups(metrics: Metric[]): MetricGroup[] {
  const groups: MetricGroup[] = [];
  for (const metric of metrics) {
    const softwareName = metric.software_name ?? null;
    const softwareVersion = metric.software_version ?? null;
    const runId = metric.run_id ?? null;
    let group = groups.find(
      (candidate) =>
        candidate.softwareName === softwareName &&
        candidate.softwareVersion === softwareVersion &&
        candidate.runId === runId,
    );
    if (!group) {
      group = {
        id: `stored-metrics:${groups.length}`,
        softwareName,
        softwareVersion,
        runId,
        metrics: [],
      };
      groups.push(group);
    }
    group.metrics.push(metric);
  }
  return groups;
}

/** The metric set the request carries, without ids. A locked metric, and
 *  one the form has no field for, keeps its stored value and provenance. */
function metricValues(
  model: ModelDetail,
  drafts: MetricDraft[],
  locked: Partial<Record<MetricKey, ArchiveLock>>,
): Omit<CreateMetricRequest, "id">[] {
  const stored = model.metrics ?? [];
  const runByGroup = new Map(
    metricGroups(stored).map((group) => [group.id, group.runId]),
  );
  const formKeys = new Set<string>(METRIC_FIELDS.map((field) => field.key));
  const values: Omit<CreateMetricRequest, "id">[] = stored
    .filter((metric) => locked[metric.key] || !formKeys.has(metric.key))
    .map((metric) => ({
      key: metric.key,
      value: metric.value,
      software_name: metric.software_name ?? null,
      software_version: metric.software_version ?? null,
      run_id: metric.run_id ?? null,
    }));
  for (const draft of drafts) {
    for (const field of METRIC_FIELDS) {
      if (locked[field.key]) {
        continue;
      }
      const value = optionalNumber(draft.values[field.key] ?? "");
      if (value === undefined) {
        continue;
      }
      values.push({
        key: field.key,
        value,
        software_name: draft.softwareName?.trim() || null,
        software_version: draft.softwareVersion?.trim() || null,
        run_id: runByGroup.get(draft.id) ?? null,
      });
    }
  }
  return values;
}

// ------------------------------------------------------------- helpers --

/** True when a change carries at least one key; change_summary alone does
 *  not count. */
export function hasChanges(change: object): boolean {
  return Object.keys(change).some((key) => key !== "change_summary");
}

/** The keys of after whose values differ from before. */
function changedValues<Values extends object>(before: Values, after: Values): Partial<Values> {
  const changed: Partial<Values> = {};
  for (const key of Object.keys(after) as (keyof Values)[]) {
    if (!sameJSON(before[key], after[key])) {
      changed[key] = after[key];
    }
  }
  return changed;
}

/** Equal as JSON: undefined and absent keys are the same, key order is not. */
export function sameJSON(left: unknown, right: unknown): boolean {
  if (Array.isArray(left) || Array.isArray(right)) {
    return (
      Array.isArray(left) &&
      Array.isArray(right) &&
      left.length === right.length &&
      left.every((item, index) => sameJSON(item, right[index]))
    );
  }
  if (isRecord(left) && isRecord(right)) {
    const keys = new Set(
      [...Object.keys(left), ...Object.keys(right)].filter(
        (key) => left[key] !== undefined || right[key] !== undefined,
      ),
    );
    return [...keys].every((key) => sameJSON(left[key], right[key]));
  }
  return left === right;
}

function isRecord(value: unknown): value is JSONRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function asRecord(value: unknown): JSONRecord {
  return isRecord(value) ? value : {};
}

function setOrDelete(target: JSONRecord, key: string, value: unknown): void {
  if (value === undefined) {
    delete target[key];
  } else {
    target[key] = value;
  }
}

function setOptionalNumber(target: JSONRecord, key: string, text: string): void {
  setOrDelete(target, key, optionalNumber(text));
}

function numberText(value: number | null | undefined): string {
  return value == null ? "" : String(value);
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** A stored list of strings; undefined when the key is absent (unknown). */
function stringList(value: unknown): string[] | undefined {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : undefined;
}

function compoundReviews(extensions: unknown): CompoundReview[] {
  const items = asRecord(extensions).compound_review;
  return Array.isArray(items) ? (items as CompoundReview[]) : [];
}
