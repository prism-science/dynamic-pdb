import type { CreateEntryInput } from "@/lib/api/entries";
import { canonicalMethod } from "@/lib/parse/structure";

import type { ArchiveAutofill } from "./autofill";
import { compoundListRequest, compoundsAreUntouched, compoundsFromParsed } from "./compounds";
import { parsedCompounds, type ExtractedFile } from "./fileExtraction";
import { advice, type FieldMessage } from "./messages";
import { MUTATION_PATTERN } from "./polymerEntities";
import type { EntryMetadataDraft, ParsedFile } from "./types";

// The New entry form reads an mmCIF the depositor uploads with the entry on
// the server (POST /v1/files/extract) and fills what it states about the
// entry: its composition, how it was measured, and each entity's engineered
// mutations. Only empty fields are filled, and a value the PDB lookup fills
// stays the archive's: where the file disagrees, the form says so beside the
// field.
//
// Each filled value is kept in the shape the create request sends it, so the
// request can name the file as the value's source (value_sources) only while
// it still sends exactly that value.

/** The entry fields a structure file fills, by the create request's key. */
export type EntryFileField = "ligands" | "cofactors" | "resolution" | "method" | "space_group";

const ENTRY_FILE_POINTERS: Readonly<Record<EntryFileField, string>> = {
  ligands: "/metadata/ligands",
  cofactors: "/metadata/cofactors",
  resolution: "/metadata/resolution",
  method: "/metadata/method",
  space_group: "/metadata/space_group",
};

const ENTRY_FILE_FIELDS = Object.keys(ENTRY_FILE_POINTERS) as EntryFileField[];

/** The values one uploaded file filled, as the request sends them. */
export type EntryFileFill = {
  fileId: string;
  fileName: string;
  values: Partial<Record<EntryFileField, unknown>>;
};

/** A file the server read, kept for the notes it gives. */
export type ReadEntryFile = { fileId: string; fileName: string; extracted: ExtractedFile };

/**
 * The metadata with the file's values in each field the depositor left empty,
 * and the fill that records them. pdbId is the PDB ID typed in the form: a
 * file naming that PDB ID is the archive's own, and fills no mutations, since
 * the lookup already lists the archive's.
 */
export function withEntryFileValues(
  metadata: EntryMetadataDraft,
  file: { id: string; name: string },
  extracted: ExtractedFile,
  pdbId: string,
): { metadata: EntryMetadataDraft; fill: EntryFileFill } {
  const fill: EntryFileFill = { fileId: file.id, fileName: file.name, values: {} };
  const next = { ...metadata };

  const { parsed, needsReview } = parsedCompounds(extracted);
  if ((parsed.ligands || parsed.cofactors) && compoundsAreUntouched(metadata.compounds)) {
    next.compounds = compoundsFromParsed(parsed, needsReview);
    fill.values.ligands = compoundListRequest(next.compounds.ligands);
    fill.values.cofactors = compoundListRequest(next.compounds.cofactors);
  }
  if (extracted.resolution != null && !metadata.resolution.trim()) {
    next.resolution = String(extracted.resolution);
    fill.values.resolution = extracted.resolution;
  }
  const method = fileMethod(extracted);
  if (method && !metadata.method.trim()) {
    next.method = method;
    fill.values.method = method;
  }
  if (extracted.spaceGroup && !metadata.spaceGroup.trim()) {
    next.spaceGroup = extracted.spaceGroup;
    fill.values.space_group = extracted.spaceGroup;
  }
  if (extracted.mutations && extracted.entryId !== pdbId) {
    next.polymerEntities = metadata.polymerEntities.map((entity) => {
      const set = extracted.mutations?.find((candidate) => candidate.entityId === entity.labelEntityId);
      const usable =
        set &&
        !set.needsReview &&
        set.mutations.length > 0 &&
        set.mutations.every((mutation) => MUTATION_PATTERN.test(mutation));
      return usable && entity.mutations.length === 0 ? { ...entity, mutations: set.mutations } : entity;
    });
  }
  return { metadata: next, fill };
}

/** The fills that still hold: the request sends the filled value, and the file
 *  is still among the entry's files with its checksum. */
export function liveEntryFills(
  input: CreateEntryInput,
  fills: readonly EntryFileFill[],
  files: readonly ParsedFile[],
): Partial<Record<EntryFileField, EntryFileFill>> {
  const live: Partial<Record<EntryFileField, EntryFileFill>> = {};
  for (const fill of fills) {
    if (!files.some((file) => file.id === fill.fileId && file.sha256)) {
      continue;
    }
    for (const field of ENTRY_FILE_FIELDS) {
      if (field in fill.values && sameValue(fill.values[field], requestValue(input, field))) {
        live[field] = fill;
      }
    }
  }
  return live;
}

/** The create request's value_sources for the fills that hold. */
export function entryValueSourcesRequest(
  input: CreateEntryInput,
  fills: readonly EntryFileFill[],
  files: readonly ParsedFile[],
): Record<string, { artifact_id: string }> | undefined {
  const live = liveEntryFills(input, fills, files);
  const sources: Record<string, { artifact_id: string }> = {};
  for (const field of ENTRY_FILE_FIELDS) {
    const fill = live[field];
    if (fill) {
      sources[ENTRY_FILE_POINTERS[field]] = { artifact_id: fill.fileId };
    }
  }
  return Object.keys(sources).length > 0 ? sources : undefined;
}

/** "from deposit.cif" beside each field a file's value still fills. */
export function entryFileBadges(
  input: CreateEntryInput,
  fills: readonly EntryFileFill[],
  files: readonly ParsedFile[],
): Partial<Record<EntryFileField, string>> {
  const badges: Partial<Record<EntryFileField, string>> = {};
  for (const [field, fill] of Object.entries(liveEntryFills(input, fills, files))) {
    badges[field as EntryFileField] = `from ${fill.fileName}`;
  }
  return badges;
}

/**
 * Notes on what the files say beside what the form shows: a value the PDB
 * lookup fills differently, and a file that names a PDB ID other than the one
 * typed (D5). Advice only: the depositor decides.
 */
export function entryFileMessages(
  files: readonly ReadEntryFile[],
  archive: ArchiveAutofill | null,
  pdbId: string,
): FieldMessage[] {
  const messages: FieldMessage[] = [];
  for (const { fileName, extracted } of files) {
    if (extracted.entryId && pdbId && extracted.entryId !== pdbId) {
      // Each note starts with "the file": the message formatter capitalises its first letter.
      messages.push(advice("entry.pdb", `the file ${fileName} names PDB ${extracted.entryId}, not ${pdbId}`));
    }
    if (!archive) {
      continue;
    }
    const method = fileMethod(extracted);
    const differences: [keyof typeof FIELD_IDS, unknown, unknown][] = [
      ["resolution", extracted.resolution, archive.request.resolution],
      ["method", method, archive.request.method],
      ["space_group", extracted.spaceGroup, archive.request.space_group],
    ];
    for (const [field, fileValue, archiveValue] of differences) {
      if (fileValue != null && archiveValue != null && !sameValue(fileValue, archiveValue)) {
        messages.push(
          advice(FIELD_IDS[field], `the file ${fileName} gives ${String(fileValue)}; the form uses the PDB's value`),
        );
      }
    }
  }
  return messages;
}

/** A PDB ID a file names when none is typed, for the form to offer to look up. */
export function pdbSuggestion(
  files: readonly ReadEntryFile[],
  pdbId: string,
): { pdbId: string; fileName: string } | null {
  if (pdbId) {
    return null;
  }
  const named = files.find((file) => file.extracted.entryId);
  return named?.extracted.entryId ? { pdbId: named.extracted.entryId, fileName: named.fileName } : null;
}

const FIELD_IDS = {
  resolution: "entry.resolution",
  method: "entry.method",
  space_group: "entry.spaceGroup",
} as const;

/** The method as the form offers it ("X-ray crystallography"), or the file's
 *  own text for a method the form does not list. */
function fileMethod(extracted: ExtractedFile): string | null {
  if (!extracted.method) {
    return null;
  }
  return canonicalMethod(extracted.method) ?? extracted.method;
}

function requestValue(input: CreateEntryInput, field: EntryFileField): unknown {
  switch (field) {
    case "ligands":
      return input.ligands ?? undefined;
    case "cofactors":
      return input.cofactors ?? undefined;
    case "resolution":
      return input.resolution ?? undefined;
    case "method":
      return input.method || undefined;
    case "space_group":
      return input.space_group ?? undefined;
    default:
      throw new Error(`unknown entry file field: ${String(field)}`);
  }
}

function sameValue(first: unknown, second: unknown): boolean {
  return JSON.stringify(first) === JSON.stringify(second);
}
