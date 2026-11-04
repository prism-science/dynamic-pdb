import type {
  ArchiveEntryProposal,
  ArchivePolymerEntityProposal,
} from "@/lib/api/entries";

import type { FieldId } from "./fields";
import { advice, type FieldMessage } from "./messages";
import { mutationsNeedingReview } from "./polymerEntities";

// What the PDB lookup says about its own values: where RCSB reports more than
// one value for a field (conflicts), and what could not be read or breaks a
// schema rule (warnings). The form shows each one beside the field it is
// about, as advice: none of them blocks the submit, because the depositor can
// only take the field over, not fix the archive.

type ArchiveConflict = ArchiveEntryProposal["conflicts"][number];
type ArchiveWarning = ArchiveEntryProposal["warnings"][number];

/** A finished lookup: the entry, its polymer entities, and the warnings about
 *  the lookup as a whole (such as an unknown UniProt release). */
export type ArchiveLookupReview = {
  entry: ArchiveEntryProposal;
  polymerEntities: ArchivePolymerEntityProposal[];
  warnings: ArchiveWarning[];
};

/**
 * The form field each proposed entry field is shown in. The backend names the
 * fields (lib/archive) and may add more, so a name not listed here is not an
 * error: its note goes beside the PDB ID, with the other notes about the
 * lookup as a whole (status, dates, revision history).
 */
const ENTRY_FIELDS: ReadonlyMap<string, FieldId> = new Map<string, FieldId>([
  ["title", "entry.title"],
  ["details", "entry.details"],
  ["resolution", "entry.resolution"],
  ["method", "entry.method"],
  ["space_group", "entry.spaceGroup"],
]);
const ENTRY_FALLBACK_FIELD: FieldId = "entry.pdb";

/** The same for a polymer entity; an unlisted name goes to the entity's card. */
const ENTITY_FIELDS: ReadonlyMap<string, FieldId> = new Map<string, FieldId>([
  ["source_organisms", "entity.organism"],
  ["construct", "entity.construct"],
  ["mutations", "entity.mutations"],
  ["mutations_raw", "entity.mutations"],
  ["mutations_status", "entity.mutations"],
  ["uniprot_mappings", "entity.uniprot"],
  ["alignment_mappings", "entity.uniprot"],
]);
const ENTITY_FALLBACK_FIELD: FieldId = "entity.block";

/** Beside an archive mutation text marked "needs review" (mutations_status
 *  needs_review): the form shows the text as written, and only the depositor
 *  can say which substitutions it means. */
export const UNCLEAR_MUTATIONS_ADVICE =
  "part of the PDB's mutation text is not a single substitution, so it needs review: type the substitutions you mean";

/** Every conflict and warning of a finished lookup of pdbId, as advice beside
 *  the field it is about, and a note on each mutation text that needs review. */
export function lookupReviewMessages(
  pdbId: string,
  review: ArchiveLookupReview,
): FieldMessage[] {
  const messages: FieldMessage[] = review.warnings.map((warning) =>
    advice(ENTRY_FALLBACK_FIELD, warningText(pdbId, warning)),
  );
  messages.push(
    ...proposalMessages(pdbId, review.entry, (name) => entryField(name), null),
  );
  for (const entity of review.polymerEntities) {
    // An entity without a label has no card in the form to show notes on.
    const item = entity.label_entity_id?.trim();
    if (item) {
      messages.push(...proposalMessages(pdbId, entity, (name) => entityField(name), item));
      if (mutationsNeedingReview(entity)) {
        messages.push(advice("entity.mutations", UNCLEAR_MUTATIONS_ADVICE, item));
      }
    }
  }
  return messages;
}

function proposalMessages(
  pdbId: string,
  proposal: Pick<ArchiveEntryProposal, "conflicts" | "warnings">,
  fieldOf: (name: string) => FieldId,
  item: string | null,
): FieldMessage[] {
  return [
    ...proposal.conflicts.map((conflict) =>
      advice(fieldOf(conflict.field), conflictText(pdbId, conflict), item),
    ),
    ...proposal.warnings.map((warning) =>
      advice(fieldOf(warningFieldName(warning)), warningText(pdbId, warning), item),
    ),
  ];
}

function entryField(name: string): FieldId {
  return ENTRY_FIELDS.get(name) ?? ENTRY_FALLBACK_FIELD;
}

function entityField(name: string): FieldId {
  return ENTITY_FIELDS.get(name) ?? ENTITY_FALLBACK_FIELD;
}

/**
 * The proposed field a warning is about: an archive warning names it, a
 * validation warning points into the proposal ("/resolution",
 * "/uniprot_mappings/0/accession"), so its first segment is the field.
 */
function warningFieldName(warning: ArchiveWarning): string {
  switch (warning.source) {
    case "archive":
      return warning.field ?? "";
    case "validation":
      return (warning.pointer ?? "").split("/")[1] ?? "";
    default:
      throw new Error(`unknown archive warning source: ${String(warning.source)}`);
  }
}

/** "PDB 7L0B lists more than one method: X-RAY DIFFRACTION, NEUTRON
 *  DIFFRACTION. The form fills in the first" */
export function conflictText(pdbId: string, conflict: ArchiveConflict): string {
  const values = [conflict.chosen, ...conflict.others].join(", ");
  return `PDB ${pdbId} lists more than one ${fieldWords(conflict.field)}: ${values}. The form fills in the first`;
}

export function warningText(pdbId: string, warning: ArchiveWarning): string {
  switch (warning.source) {
    case "archive":
      return `PDB ${pdbId}: ${warning.detail}`;
    case "validation":
      return `the value from PDB ${pdbId} breaks a schema rule: ${warning.detail}`;
    default:
      throw new Error(`unknown archive warning source: ${String(warning.source)}`);
  }
}

/** "space_group" → "space group". */
function fieldWords(name: string): string {
  return name.replaceAll("_", " ");
}
