import type { ModeledCompounds } from "@/lib/api/entries";
import { CIF_REFERENCE } from "@/lib/parse/cifReference.gen";

/**
 * The compounds a model's file models, as the pages show them (T3-M): one row
 * per compound with how many copies the file holds, each tagged from the
 * entry's own lists. The file's record keeps buffers and applies no rule; the
 * tags are where the entry's judgments come in.
 */

export type CompoundTag = "Ligand" | "Cofactor" | "Buffer";

/** The entry's Ligands and Cofactors lists, which the tags come from. */
export type EntryCompoundLists = {
  ligands: readonly string[];
  cofactors: readonly string[];
};

export type CompoundCopyView = {
  /** Chain, residue number and insertion code, as viewers show them: "A 301". */
  position: string;
  reviewReason: string | null;
};

export type CompoundGroup = {
  ccdId: string;
  copies: CompoundCopyView[];
  tags: CompoundTag[];
  /** The reasons of the copies marked for review, each once. */
  reviewReasons: string[];
};

export type ModeledCompoundsView =
  /** Nothing was read from a file: the model's typed lists stand. */
  | { state: "typed" }
  | { state: "reading" }
  | { state: "failed"; reason: string }
  | { state: "read"; groups: CompoundGroup[]; complete: boolean; warnings: string[] };

/** The ID lists the reference data calls buffers: the solvents and the
 *  compounds that are often from the buffer (sulfate, phosphate, ...). */
const BUFFER_IDS = new Set<string>([...CIF_REFERENCE.solvents, ...CIF_REFERENCE.ambiguous]);

/**
 * What the page shows for a model's compounds. A model whose file the server
 * does not hold (not_stored), or a response without the record, keeps its typed
 * lists. Without the entry's lists (a page that did not load the entry), the
 * groups carry no tags rather than guessed ones.
 */
export function modeledCompoundsView(
  compounds: ModeledCompounds | undefined,
  entryLists: EntryCompoundLists | null,
): ModeledCompoundsView {
  if (!compounds) {
    return { state: "typed" };
  }
  switch (compounds.status) {
    case "not_stored":
      return { state: "typed" };
    case "pending":
      return { state: "reading" };
    case "failed":
      return { state: "failed", reason: compounds.last_error ?? "no reason was recorded" };
    case "done":
      return {
        state: "read",
        groups: groupCopies(compounds.copies, entryLists),
        complete: compounds.complete !== false,
        warnings: compounds.warnings,
      };
    default:
      throw new Error(`unknown modeled compounds status: ${String(compounds.status satisfies never)}`);
  }
}

/**
 * The tags of one compound: Ligand when the entry's Ligands list names it,
 * Cofactor when its Cofactors list does (both when both do, as for a zinc
 * that is a ligand in one site and a cofactor in another), otherwise Buffer
 * when the reference lists call it one. A compound on none of them has no tag:
 * a buffer is never guessed.
 */
export function compoundTags(ccdId: string, entryLists: EntryCompoundLists): CompoundTag[] {
  const tags: CompoundTag[] = [];
  if (entryLists.ligands.includes(ccdId)) {
    tags.push("Ligand");
  }
  if (entryLists.cofactors.includes(ccdId)) {
    tags.push("Cofactor");
  }
  if (tags.length === 0 && BUFFER_IDS.has(ccdId)) {
    tags.push("Buffer");
  }
  return tags;
}

/** Copies grouped by compound, in the record's order (CCD ID, then position). */
function groupCopies(
  copies: ModeledCompounds["copies"],
  entryLists: EntryCompoundLists | null,
): CompoundGroup[] {
  const groups = new Map<string, CompoundGroup>();
  for (const copied of copies) {
    let group = groups.get(copied.ccd_id);
    if (!group) {
      group = {
        ccdId: copied.ccd_id,
        copies: [],
        tags: entryLists ? compoundTags(copied.ccd_id, entryLists) : [],
        reviewReasons: [],
      };
      groups.set(copied.ccd_id, group);
    }
    const reviewReason = copied.needs_review ? (copied.review_reason ?? "needs review") : null;
    group.copies.push({ position: copyPosition(copied), reviewReason });
    if (reviewReason !== null && !group.reviewReasons.includes(reviewReason)) {
      group.reviewReasons.push(reviewReason);
    }
  }
  return [...groups.values()];
}

/** "A 301", "A 301A" with an insertion code, "301" without a chain. */
function copyPosition(copied: ModeledCompounds["copies"][number]): string {
  const residue = `${copied.auth_seq_id ?? ""}${copied.pdb_ins_code ?? ""}`;
  const position = [copied.auth_asym_id ?? "", residue].filter(Boolean).join(" ");
  return position || "position not given";
}
