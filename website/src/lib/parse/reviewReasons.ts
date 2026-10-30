// Review reasons of the ligand rule: the same text as lib/cif (sorting.go,
// reference.go, annotations.go). The parity test (parity-golden.json) fails
// when they drift. A module of its own, so the entry form can show a reason
// without loading the rule and its CCD table.

export const DRAFT_SOLVENT_REASON =
  "compound is on the draft solvent list, which researchers have not approved: kept as a ligand instead of dropped";
export const SINGLE_ATOM_REASON =
  "single-atom compound is not in the metal ion reference list: confirm whether it is a metal ion";
export const NO_METALC_ROWS_REASON =
  "metal ion kept for review: the file has no metalc connection rows";
export const UNLINKED_METAL_REASON =
  "metal ion instance has no metalc connection to a protein residue or ligand";
export const WATER_ONLY_METAL_REASON = "metal linked only to water: confirm the site";
export const OUTSIDE_CHAIN_REASON = "residue outside the polymer chain: confirm it is a ligand";
/** Also the hint the entry form gives a typed ambiguous ID. */
export const AMBIGUOUS_COMPOUND_REASON =
  "often from the buffer: confirm it is part of the sample";
