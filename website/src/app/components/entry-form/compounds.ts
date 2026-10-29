import { CIF_REFERENCE } from "@/lib/parse/cifReference.gen";
import { AMBIGUOUS_COMPOUND_REASON, type CompoundReview } from "@/lib/parse/structure";

import type {
  CompoundList,
  CompoundListMode,
  CompoundListName,
  CompoundsDraft,
} from "./types";

// Ligands and cofactors as the depositor types them. The sorting uses the
// reference lists the parsers use (lib/cif/*.json), so a typed HEM lands
// where a parsed one would.

/** A CCD chemical-component ID: up to five capital letters or digits. */
export const CCD_ID_PATTERN = /^[A-Z0-9]{1,5}$/;

const WATER_COMPOUNDS = new Set(["HOH", "DOD", "WAT", "H2O"]);
const COFACTOR_COMPOUNDS = new Set<string>(CIF_REFERENCE.cofactors);
const METAL_IONS = new Set<string>(CIF_REFERENCE.metalIons);
const SOLVENT_COMPOUNDS = new Set<string>(CIF_REFERENCE.solvents);
const AMBIGUOUS_COMPOUNDS = new Set<string>(CIF_REFERENCE.ambiguous);

const SOLVENT_REVIEW =
  "usually a solvent or buffer, which the sample composition leaves out";

/** The hint a typed ID gets: the parsers' reason for an ambiguous compound
 *  (sulfate, phosphate, citrate, halides), the solvent hint for a compound the
 *  parsers drop, none otherwise. */
function typedCompoundReview(id: string): string | null {
  if (AMBIGUOUS_COMPOUNDS.has(id)) {
    return AMBIGUOUS_COMPOUND_REASON;
  }
  return SOLVENT_COMPOUNDS.has(id) ? SOLVENT_REVIEW : null;
}

export const COMPOUND_LIST_NAMES: CompoundListName[] = ["ligands", "cofactors"];

export function otherCompoundList(list: CompoundListName): CompoundListName {
  switch (list) {
    case "ligands":
      return "cofactors";
    case "cofactors":
      return "ligands";
    default:
      throw new Error(`unknown compound list: ${String(list)}`);
  }
}

/** Kept metals go with the cofactors: the schema has no third list. Water
 *  belongs in neither. */
export function compoundListFor(id: string): CompoundListName | null {
  if (WATER_COMPOUNDS.has(id)) {
    return null;
  }
  return COFACTOR_COMPOUNDS.has(id) || METAL_IONS.has(id)
    ? "cofactors"
    : "ligands";
}

export function normalizeCompoundId(raw: string): string {
  return raw.trim().toUpperCase();
}

export function compoundIdError(id: string): string | null {
  if (!CCD_ID_PATTERN.test(id)) {
    return `${id} is not a CCD ID: use up to five letters or digits, for example ATP`;
  }
  if (WATER_COMPOUNDS.has(id)) {
    return "water is not part of the sample composition";
  }
  return null;
}

export function hasCompound(compounds: CompoundsDraft, id: string): boolean {
  return COMPOUND_LIST_NAMES.some((list) =>
    compounds[list].items.some((item) => item.id === id),
  );
}

export type AddCompoundResult = {
  compounds: CompoundsDraft;
  error: string | null;
};

export function addCompound(
  compounds: CompoundsDraft,
  raw: string,
): AddCompoundResult {
  const id = normalizeCompoundId(raw);
  if (!id) {
    return { compounds, error: null };
  }
  const error = compoundIdError(id);
  if (error) {
    return { compounds, error };
  }
  if (hasCompound(compounds, id)) {
    return { compounds, error: `${id} is already listed` };
  }
  const list = compoundListFor(id);
  if (!list) {
    return { compounds, error: "water is not part of the sample composition" };
  }
  const review = typedCompoundReview(id);
  return {
    compounds: {
      ...compounds,
      [list]: {
        mode: "list",
        items: [...compounds[list].items, { id, review }],
      },
    },
    error: null,
  };
}

export type AddTypedCompoundsResult = AddCompoundResult & {
  /** The refused IDs, joined by spaces, to leave in the box for fixing. */
  rejected: string;
};

/** Adds each ID of a typed text, split on spaces, commas or semicolons. The
 *  error is the first refused ID's. */
export function addTypedCompounds(
  compounds: CompoundsDraft,
  text: string,
): AddTypedCompoundsResult {
  let next = compounds;
  const rejected: string[] = [];
  let firstError: string | null = null;
  for (const raw of text.split(/[\s,;]+/).filter(Boolean)) {
    const result = addCompound(next, raw);
    next = result.compounds;
    if (result.error) {
      rejected.push(raw);
      firstError ??= result.error;
    }
  }
  return { compounds: next, rejected: rejected.join(" "), error: firstError };
}

export function removeCompound(
  compounds: CompoundsDraft,
  list: CompoundListName,
  id: string,
): CompoundsDraft {
  return {
    ...compounds,
    [list]: {
      ...compounds[list],
      items: compounds[list].items.filter((item) => item.id !== id),
    },
  };
}

/** Moving a compound is the depositor's decision about it, so it also clears
 *  the parser's doubt. */
export function moveCompound(
  compounds: CompoundsDraft,
  from: CompoundListName,
  id: string,
): CompoundsDraft {
  const to = otherCompoundList(from);
  const removed = removeCompound(compounds, from, id);
  return {
    ...removed,
    [to]: {
      mode: "list",
      items: [...compounds[to].items, { id, review: null }],
    },
  };
}

export function confirmCompound(
  compounds: CompoundsDraft,
  list: CompoundListName,
  id: string,
): CompoundsDraft {
  return {
    ...compounds,
    [list]: {
      ...compounds[list],
      items: compounds[list].items.map((item) =>
        item.id === id ? { ...item, review: null } : item,
      ),
    },
  };
}

/** "Not known" and "None" both empty the list: neither has members. */
export function setCompoundListMode(
  compounds: CompoundsDraft,
  list: CompoundListName,
  mode: CompoundListMode,
): CompoundsDraft {
  switch (mode) {
    case "unknown":
    case "none":
      return { ...compounds, [list]: { mode, items: [] } };
    case "list":
      return { ...compounds, [list]: { ...compounds[list], mode } };
    default:
      throw new Error(`unknown compound list mode: ${String(mode)}`);
  }
}

/** Omitted means not known, and an empty list means none. */
export function compoundListRequest(list: CompoundList): string[] | undefined {
  switch (list.mode) {
    case "unknown":
      return undefined;
    case "none":
      return [];
    case "list":
      return list.items.map((item) => item.id);
    default:
      throw new Error(`unknown compound list mode: ${String(list.mode)}`);
  }
}

/** The doubts still open, in the shape the parsers send them on a model's
 *  `extensions.compound_review`. */
export function compoundReviewRequest(compounds: CompoundsDraft): CompoundReview[] {
  return COMPOUND_LIST_NAMES.flatMap((list) =>
    compounds[list].items.flatMap((item) =>
      item.review ? [{ list, id: item.id, reason: item.review }] : [],
    ),
  );
}

export function compoundsAreUntouched(compounds: CompoundsDraft): boolean {
  return COMPOUND_LIST_NAMES.every(
    (list) =>
      compounds[list].mode === "unknown" && compounds[list].items.length === 0,
  );
}

/** A coordinate file's composition as the form edits it. A list the parser
 *  did not report stays not known; a reported empty list means none. */
export function compoundsFromParsed(
  parsed: { ligands?: string[]; cofactors?: string[] },
  needsReview: CompoundReview[],
): CompoundsDraft {
  const reviewReason = (list: CompoundListName, id: string) =>
    needsReview.find((item) => item.list === list && item.id === id)?.reason ??
    null;
  const fromList = (list: CompoundListName): CompoundList => {
    const ids = parsed[list];
    if (!ids) {
      return { mode: "unknown", items: [] };
    }
    return {
      mode: ids.length > 0 ? "list" : "none",
      items: ids.map((id) => ({ id, review: reviewReason(list, id) })),
    };
  };
  return { ligands: fromList("ligands"), cofactors: fromList("cofactors") };
}
