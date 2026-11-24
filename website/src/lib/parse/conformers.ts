import { loopColumn, type CifLoop } from "./cif";

/**
 * Conformer counting, as lib/cif counts it (`lib/cif/conformers.go`), shared by the deposit
 * preview's altloc_fraction and the viewer's conformer track. The researchers asked for one rule
 * for conformer_count and altloc_fraction (17 Nov, Q1):
 *
 * - A residue's conformer count is the number of its alternate location letters whose highest
 *   occupancy on a heavy atom of the residue is at least 0.1; a residue with none has 1.
 * - Letters found only on hydrogens never count; atoms without a letter are not a position.
 * - altloc_fraction is the share of the first model's polymer residues with 2 or more.
 *
 * The parity golden (`lib/cif/testdata/parity-golden.json`) holds Go's altloc_fraction for every
 * parity file, and `test/cif-parity.test.cjs` checks this code gives the same.
 */

export const MIN_CONFORMER_OCCUPANCY = 0.1;

/** lib/cif keeps at most this many alternate location letters a residue. */
const MAX_ALT_POSITIONS_PER_RESIDUE = 16;
/** lib/cif's bound on the polymer residues it keeps (maxIndexedResidues). */
const MAX_INDEXED_RESIDUES = 1_000_000;
/** lib/cif's bound on distinct SEQRES names (maxSequenceNames). */
const MAX_SEQUENCE_NAMES = 100_000;
/** lib/cif's bound on HETATM records waiting for their chain (maxPendingAtoms). */
const MAX_PENDING_ATOMS = 200_000;
/** lib/cif's bound on a copied field (maxCopyFieldBytes). */
const MAX_FIELD_BYTES = 64;

const WATER = new Set(["HOH", "DOD", "WAT", "H2O"]);

/** A hydrogen (or deuterium) by its element, or, without one, by its atom name: an H or D after
 *  any leading digits ("1HB", "HD21"). */
export function isHydrogenAtom(element: string, atomName: string): boolean {
  const upper = element.trim().toUpperCase();
  if (upper === "H" || upper === "D") {
    return true;
  }
  if (upper !== "") {
    return false;
  }
  const name = atomName.trim().toUpperCase().replace(/^[0-9]+/, "");
  return name.startsWith("H") || name.startsWith("D");
}

/** The conformer count of a residue: its letters at or above the floor, minimum 1.
 *  `highestHeavyOccupancy` maps each letter to its highest occupancy on a heavy atom. */
export function conformerCount(highestHeavyOccupancy: ReadonlyMap<string, number>): number {
  let counted = 0;
  for (const occupancy of highestHeavyOccupancy.values()) {
    if (occupancy >= MIN_CONFORMER_OCCUPANCY) {
      counted += 1;
    }
  }
  return Math.max(1, counted);
}

/** An occupancy as lib/cif reads it: missing or unreadable is full occupancy. */
export function occupancyValue(raw: string): number {
  const value = Number.parseFloat(raw);
  return raw.trim() === "" || !Number.isFinite(value) ? 1 : value;
}

/** lib/cif's plainCopyText: at most 64 bytes of text without control characters. */
function plainText(...values: string[]): boolean {
  for (const value of values) {
    if (/[\u0000-\u001f\u007f-\u009f]/.test(value)) {
      return false;
    }
    const bytes = /^[\u0000-\u007f]*$/.test(value) ? value.length : new TextEncoder().encode(value).length;
    if (bytes > MAX_FIELD_BYTES) {
      return false;
    }
  }
  return true;
}

/** One coordinate row of a polymer residue. */
interface PolymerAtom {
  firstModel: boolean;
  chain: string;
  authSeq: string;
  insCode: string;
  compound: string;
  labelAsym: string;
  labelSeq: string;
  altID: string;
  atomName: string;
  element: string;
  occupancy: string;
}

/** The first model's polymer residues with the highest heavy-atom occupancy of each letter. */
export class ConformerTally {
  private readonly residues = new Map<string, Map<string, number>>();
  /** Set once the residues ran past lib/cif's bound: no altloc_fraction then. */
  overflow = false;

  add(atom: PolymerAtom): void {
    if (!atom.firstModel || this.overflow) {
      return;
    }
    if (!plainText(atom.labelAsym, atom.labelSeq, atom.altID, atom.atomName, atom.element, atom.occupancy)) {
      return;
    }
    const key =
      atom.labelAsym !== "" && atom.labelSeq !== ""
        ? `L\u0000${atom.labelAsym}\u0000${atom.labelSeq}\u0000${atom.compound}`
        : `A\u0000${atom.chain}\u0000${atom.authSeq}\u0000${atom.insCode}\u0000${atom.compound}`;
    let letters = this.residues.get(key);
    if (letters === undefined) {
      if (this.residues.size >= MAX_INDEXED_RESIDUES) {
        this.overflow = true;
        return;
      }
      letters = new Map<string, number>();
      this.residues.set(key, letters);
    }
    if (atom.altID === "" || isHydrogenAtom(atom.element, atom.atomName)) {
      return;
    }
    const highest = letters.get(atom.altID);
    if (highest === undefined && letters.size >= MAX_ALT_POSITIONS_PER_RESIDUE) {
      return;
    }
    const occupancy = occupancyValue(atom.occupancy);
    if (highest === undefined || occupancy > highest) {
      letters.set(atom.altID, occupancy);
    }
  }

  /** Residues with 2 or more conformers over the residues counted; null when none was counted. */
  altlocFraction(): number | null {
    if (this.overflow || this.residues.size === 0) {
      return null;
    }
    let multiConformer = 0;
    for (const letters of this.residues.values()) {
      if (conformerCount(letters) >= 2) {
        multiConformer += 1;
      }
    }
    return multiConformer / this.residues.size;
  }
}

/** The polymer-residue rule for mmCIF rows, as lib/cif's coordinate index reads them: ATOM rows,
 *  and HETATM rows with a label_seq_id; without a group_PDB column, rows with a label_seq_id. Water
 *  never counts; the first model is the one of the first row. */
export class MmcifPolymerReader {
  private readonly column: Record<string, number>;
  private readonly noGroupColumn: boolean;
  private firstModel: string | undefined;

  constructor(
    loop: CifLoop,
    private readonly tally: ConformerTally,
  ) {
    const names = [
      "group_pdb", "label_comp_id", "auth_comp_id", "auth_asym_id", "label_asym_id", "auth_seq_id",
      "label_seq_id", "pdbx_pdb_ins_code", "label_entity_id", "pdbx_pdb_model_num", "label_alt_id",
      "label_atom_id", "auth_atom_id", "type_symbol", "occupancy",
    ];
    this.column = Object.fromEntries(names.map((name) => [name, loopColumn(loop, name)]));
    this.noGroupColumn = this.column.group_pdb === -1;
  }

  /** lib/cif's cell: trimmed, with "." and "?" read as empty. */
  private cell(row: string[], name: string): string {
    const index = this.column[name];
    const value = index === -1 ? undefined : row[index]?.trim();
    return value === undefined || value === "." || value === "?" ? "" : value;
  }

  add(row: string[]): void {
    const model = this.cell(row, "pdbx_pdb_model_num");
    if (this.firstModel === undefined) {
      this.firstModel = model;
    }
    const labelSeq = this.cell(row, "label_seq_id");
    if (!this.isPolymerRow(row, labelSeq !== "")) {
      return;
    }
    const compound = (this.cell(row, "label_comp_id") || this.cell(row, "auth_comp_id")).toUpperCase();
    if (compound === "" || WATER.has(compound)) {
      return;
    }
    const chain = this.cell(row, "auth_asym_id") || this.cell(row, "label_asym_id");
    const authSeq = this.cell(row, "auth_seq_id") || labelSeq;
    const insCode = this.cell(row, "pdbx_pdb_ins_code");
    if (chain === "" || authSeq === "" || !plainText(chain, this.cell(row, "label_entity_id"), authSeq, insCode, compound)) {
      return;
    }
    this.tally.add({
      firstModel: model === this.firstModel,
      chain,
      authSeq,
      insCode,
      compound,
      labelAsym: this.cell(row, "label_asym_id"),
      labelSeq,
      altID: this.cell(row, "label_alt_id"),
      atomName: this.cell(row, "label_atom_id") || this.cell(row, "auth_atom_id"),
      element: this.cell(row, "type_symbol").toUpperCase(),
      occupancy: this.cell(row, "occupancy"),
    });
  }

  private isPolymerRow(row: string[], hasLabelSeq: boolean): boolean {
    if (this.noGroupColumn) {
      return hasLabelSeq;
    }
    switch (this.cell(row, "group_pdb").toUpperCase()) {
      case "ATOM":
        return true;
      case "HETATM":
        return hasLabelSeq;
      default:
        return false;
    }
  }
}

/** The polymer-residue rule for PDB-format records, as lib/cif's pdbIndexReader applies it: ATOM
 *  records, and HETATM residues between ATOM residues of their chain or named in its SEQRES before
 *  its TER record. Feed it the first model's coordinate and TER records and every SEQRES record,
 *  then call endModel. */
export class PdbPolymerReader {
  private readonly seqresNames = new Map<string, Set<string>>();
  private nameCount = 0;
  private readonly chainHasAtoms = new Set<string>();
  private readonly terminated = new Set<string>();
  private pending = new Map<string, Array<{ atom: PolymerAtom; afterAtom: boolean }>>();
  private pendingCount = 0;
  private lastChain = "";
  /** Set once more HETATM records waited than lib/cif allows: no altloc_fraction then. */
  overflow = false;

  constructor(private readonly tally: ConformerTally) {}

  addSeqres(line: string): void {
    const chain = line.slice(11, 12).trim();
    for (const name of line.slice(19, 70).trim().split(/\s+/).filter(Boolean)) {
      const upper = name.toUpperCase();
      const names = this.seqresNames.get(chain);
      if (names?.has(upper)) {
        continue;
      }
      if (this.nameCount >= MAX_SEQUENCE_NAMES) {
        continue;
      }
      if (names) {
        names.add(upper);
      } else {
        this.seqresNames.set(chain, new Set([upper]));
      }
      this.nameCount += 1;
    }
  }

  addTer(line: string): void {
    const chain = line.slice(21, 22).trim() || this.lastChain;
    this.resolve(chain, false);
    this.terminated.add(chain);
  }

  addCoordinate(record: "ATOM" | "HETATM", line: string): void {
    const compound = line.slice(17, 20).trim().toUpperCase();
    const atom: PolymerAtom = {
      firstModel: true,
      chain: line.slice(21, 22).trim(),
      authSeq: line.slice(22, 26).trim(),
      insCode: line.slice(26, 27).trim(),
      compound,
      labelAsym: "",
      labelSeq: "",
      altID: line.slice(16, 17).trim(),
      atomName: line.slice(12, 16).trim(),
      element: line.slice(76, 78).trim().toUpperCase(),
      occupancy: line.slice(54, 60).trim(),
    };
    this.lastChain = atom.chain;
    if (compound === "" || WATER.has(compound)) {
      return;
    }
    if (atom.authSeq === "" || !plainText(atom.chain, atom.authSeq, atom.insCode, atom.compound)) {
      return;
    }
    if (record === "ATOM") {
      this.terminated.delete(atom.chain);
      this.resolve(atom.chain, true);
      this.chainHasAtoms.add(atom.chain);
      this.tally.add(atom);
      return;
    }
    if (this.terminated.has(atom.chain)) {
      return;
    }
    if (this.pendingCount >= MAX_PENDING_ATOMS) {
      this.overflow = true;
      return;
    }
    const waiting = this.pending.get(atom.chain) ?? [];
    waiting.push({ atom, afterAtom: this.chainHasAtoms.has(atom.chain) });
    this.pending.set(atom.chain, waiting);
    this.pendingCount += 1;
  }

  /** Sorts the HETATM records still waiting, as at a TER record. */
  endModel(): void {
    for (const chain of [...this.pending.keys()].sort()) {
      this.resolve(chain, false);
    }
  }

  private resolve(chain: string, byAtom: boolean): void {
    const waiting = this.pending.get(chain);
    if (waiting === undefined || waiting.length === 0) {
      return;
    }
    const names = this.seqresNames.get(chain);
    for (const held of waiting) {
      if ((byAtom && held.afterAtom) || names?.has(held.atom.compound)) {
        this.tally.add(held.atom);
      }
    }
    this.pendingCount -= waiting.length;
    this.pending.delete(chain);
  }
}
