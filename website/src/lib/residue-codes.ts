/**
 * Residue names as the one-letter codes a sequence is written in.
 *
 * Needed to line a coordinate file up against the sequence the entry records.
 * The file names each residue in three letters and numbers them however the
 * program that wrote it saw fit; the entry holds one string of one-letter
 * codes. Matching the names is what turns the file's numbering into a position
 * on that string.
 */
const ONE_LETTER: Record<string, string> = {
  ALA: "A",
  ARG: "R",
  ASN: "N",
  ASP: "D",
  CYS: "C",
  GLN: "Q",
  GLU: "E",
  GLY: "G",
  HIS: "H",
  ILE: "I",
  LEU: "L",
  LYS: "K",
  MET: "M",
  PHE: "F",
  PRO: "P",
  SER: "S",
  THR: "T",
  TRP: "W",
  TYR: "Y",
  VAL: "V",
  // Selenomethionine is the one non-standard residue common enough to matter:
  // half of all phased structures are grown with it in place of methionine,
  // and a sequence writes it as the methionine it stands in for.
  MSE: "M",
  SEC: "U",
  PYL: "O",
  UNK: "X",
  // Nucleic acids, so a DNA or RNA chain lines up too rather than scoring
  // zero and being left on the file's own numbering.
  DA: "A",
  DC: "C",
  DG: "G",
  DT: "T",
  DU: "U",
  A: "A",
  C: "C",
  G: "G",
  U: "U",
  T: "T",
  I: "I",
};

/** The one-letter code for a residue name, or null for anything unlisted. */
export function oneLetter(name: string): string | null {
  return ONE_LETTER[name.trim().toUpperCase()] ?? null;
}
