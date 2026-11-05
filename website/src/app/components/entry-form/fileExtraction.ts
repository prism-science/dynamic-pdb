import type { ArtifactExtraction, ExtractedValue } from "@/lib/api/entries";
import type { CompoundReview } from "@/lib/parse/structure";

// What the server read from an uploaded mmCIF (POST /v1/files/extract), in the
// shapes the forms fill. The server's parser is the one that decides what is
// stored (ticket 3 decision 7); the browser parser only gives the instant
// preview, so where both read a value the server's replaces the preview's.

/** A compound the server kept, with why it needs the depositor's review. */
export type ExtractedCompound = { id: string; reviewReason: string | null };

/** One entity's engineered mutations, in UniProt numbering (lib/cif MutationSet). */
export type ExtractedMutationSet = {
  entityId: string;
  accession: string | null;
  mutations: string[];
  original: string | null;
  needsReview: boolean;
};

export type ExtractedFile = {
  /** The PDB ID the file names, in capitals; null when it names none. */
  entryId: string | null;
  authors: string[] | null;
  rWork: number | null;
  rFree: number | null;
  resolution: number | null;
  /** As the file writes it, such as X-RAY DIFFRACTION. */
  method: string | null;
  spaceGroup: string | null;
  atomCount: number | null;
  modeledResidues: number | null;
  uniqueProteinChains: number | null;
  altlocFraction: number | null;
  memberCount: number | null;
  ligands: ExtractedCompound[] | null;
  cofactors: ExtractedCompound[] | null;
  mutations: ExtractedMutationSet[] | null;
  programName: string | null;
  programVersion: string | null;
  warnings: string[];
};

export function emptyExtractedFile(): ExtractedFile {
  return {
    entryId: null,
    authors: null,
    rWork: null,
    rFree: null,
    resolution: null,
    method: null,
    spaceGroup: null,
    atomCount: null,
    modeledResidues: null,
    uniqueProteinChains: null,
    altlocFraction: null,
    memberCount: null,
    ligands: null,
    cofactors: null,
    mutations: null,
    programName: null,
    programVersion: null,
    warnings: [],
  };
}

/**
 * The server's values by kind. Every name the API documents is listed
 * (ExtractedValue.name in openapi.yaml); a name it does not document is an
 * error, so a new server value is not silently dropped.
 */
export function readExtraction(extraction: ArtifactExtraction): ExtractedFile {
  const file = emptyExtractedFile();
  file.warnings = extraction.warnings.map((warning) => warning.detail);
  for (const value of extraction.values) {
    switch (value.name) {
      case "entry_id":
        file.entryId = text(value);
        break;
      case "authors":
        file.authors = texts(value);
        break;
      case "r_work":
        file.rWork = number(value);
        break;
      case "r_free":
        file.rFree = number(value);
        break;
      case "resolution":
        file.resolution = number(value);
        break;
      case "method":
        file.method = text(value);
        break;
      case "space_group":
        file.spaceGroup = text(value);
        break;
      case "atom_count":
        file.atomCount = number(value);
        break;
      case "modeled_residues":
        file.modeledResidues = number(value);
        break;
      case "unique_protein_chains":
        file.uniqueProteinChains = number(value);
        break;
      case "altloc_fraction":
        file.altlocFraction = number(value);
        break;
      case "member_count":
        file.memberCount = number(value);
        break;
      case "ligands":
        file.ligands = compounds(value);
        break;
      case "cofactors":
        file.cofactors = compounds(value);
        break;
      case "mutations":
        file.mutations = mutationSets(value);
        break;
      case "program_name":
        file.programName = text(value);
        break;
      case "program_version":
        file.programVersion = text(value);
        break;
      case "compound_sources":
      case "glycan_descriptors":
        // The server records these itself when the file is saved.
        break;
      default:
        throw new Error(`unknown extracted value: ${value.name}`);
    }
  }
  return file;
}

/** The compounds as the composition editor takes them (compoundsFromParsed). */
export function parsedCompounds(file: ExtractedFile): {
  parsed: { ligands?: string[]; cofactors?: string[] };
  needsReview: CompoundReview[];
} {
  const parsed: { ligands?: string[]; cofactors?: string[] } = {};
  const needsReview: CompoundReview[] = [];
  for (const list of ["ligands", "cofactors"] as const) {
    const items = file[list];
    if (!items) {
      continue;
    }
    parsed[list] = items.map((item) => item.id);
    for (const item of items) {
      if (item.reviewReason) {
        needsReview.push({ list, id: item.id, reason: item.reviewReason });
      }
    }
  }
  return { parsed, needsReview };
}

function text(value: ExtractedValue): string | null {
  return typeof value.value === "string" && value.value.trim() ? value.value.trim() : null;
}

function texts(value: ExtractedValue): string[] | null {
  return Array.isArray(value.value)
    ? value.value.filter((item): item is string => typeof item === "string" && item.trim() !== "")
    : null;
}

function number(value: ExtractedValue): number | null {
  return typeof value.value === "number" && Number.isFinite(value.value) ? value.value : null;
}

function compounds(value: ExtractedValue): ExtractedCompound[] | null {
  if (!Array.isArray(value.value)) {
    return null;
  }
  return value.value.flatMap((item): ExtractedCompound[] => {
    const record = item as { id?: unknown; needs_review?: unknown; review_reason?: unknown };
    if (typeof record.id !== "string" || !record.id) {
      return [];
    }
    const reason =
      record.needs_review === true
        ? typeof record.review_reason === "string" && record.review_reason
          ? record.review_reason
          : "needs review"
        : null;
    return [{ id: record.id, reviewReason: reason }];
  });
}

function mutationSets(value: ExtractedValue): ExtractedMutationSet[] | null {
  if (!Array.isArray(value.value)) {
    return null;
  }
  return value.value.flatMap((item): ExtractedMutationSet[] => {
    const record = item as {
      entity_id?: unknown;
      accession?: unknown;
      mutations?: unknown;
      original?: unknown;
      needs_review?: unknown;
    };
    if (typeof record.entity_id !== "string" || !record.entity_id) {
      return [];
    }
    return [
      {
        entityId: record.entity_id,
        accession: typeof record.accession === "string" && record.accession ? record.accession : null,
        mutations: Array.isArray(record.mutations)
          ? record.mutations.filter((mutation): mutation is string => typeof mutation === "string")
          : [],
        original: typeof record.original === "string" && record.original ? record.original : null,
        needsReview: record.needs_review === true,
      },
    ];
  });
}
