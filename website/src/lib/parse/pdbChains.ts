import { instanceKey, type ChainPosition, type CompoundInstance, type MetalcLinks } from "./ligandRule";

// The distinct chain and residue name pairs kept from SEQRES records, as
// maxSequenceNames in lib/cif/limits.go.
const MAX_SEQUENCE_NAMES = 100_000;

/**
 * Mirrors pdbChains in lib/cif/pdbchains.go: what a PDB-format file says about
 * its polymer chains and metal links. The residue names in each chain's SEQRES
 * records, the residues MODRES lists, where each chain's TER record falls, the
 * residue numbers of its ATOM records, and the LINK records. After the read,
 * position decides whether a HETATM residue is part of a chain
 * (selenomethionine) or not (a free glutamate after the chain's TER record).
 */
export class PdbChains {
  private readonly seqres = new Map<string, Set<string>>();
  private sequenceNameCount = 0;
  private readonly modified = new Set<string>();
  /** MODRES residue IDs in file order. */
  readonly modifiedNames: string[] = [];
  private readonly modifiedSeen = new Set<string>();
  private readonly terLine = new Map<string, number>();
  private readonly atomRange = new Map<string, { first: number; last: number }>();
  private readonly firstLine = new Map<string, number>();
  private lastChain = "";
  private readonly linkPairs: Array<[CompoundInstance, CompoundInstance]> = [];
  private readonly linkSeen = new Set<string>();

  /** A SEQRES record: the chain and the residue names in columns 20 to 70
   *  (13 slots); anything past column 70 is not a residue name. */
  addSeqres(line: string): void {
    const chain = line.slice(11, 12).trim();
    for (const name of line.slice(19, 70).trim().split(/\s+/).filter(Boolean)) {
      const upper = name.toUpperCase();
      const names = this.seqres.get(chain);
      if (names?.has(upper)) {
        continue;
      }
      if (this.sequenceNameCount >= MAX_SEQUENCE_NAMES) {
        return;
      }
      if (names) {
        names.add(upper);
      } else {
        this.seqres.set(chain, new Set([upper]));
      }
      this.sequenceNameCount += 1;
    }
  }

  /** A MODRES record: a modified residue of a chain. */
  addModres(line: string): void {
    const instance: CompoundInstance = {
      compound: line.slice(12, 15).trim().toUpperCase(),
      asym: line.slice(16, 17).trim(),
      seq: line.slice(18, 22).trim(),
      ins: line.slice(22, 23).trim(),
    };
    if (!instance.compound) {
      return;
    }
    this.modified.add(instanceKey(instance));
    if (!this.modifiedSeen.has(instance.compound)) {
      this.modifiedSeen.add(instance.compound);
      this.modifiedNames.push(instance.compound);
    }
  }

  /** A TER record ends a chain's polymer block; one without a chain ends the
   *  chain of the coordinates just read. */
  addTer(line: string, lineNumber: number): void {
    const chain = line.slice(21, 22).trim() || this.lastChain;
    if (!this.terLine.has(chain)) {
      this.terLine.set(chain, lineNumber);
    }
  }

  /** The chain of a coordinate record, the residue span of ATOM records, and
   *  the line of each instance's first HETATM record. */
  addCoordinate(record: "ATOM  " | "HETATM", line: string, lineNumber: number, instance: CompoundInstance): void {
    const chain = line.slice(21, 22).trim();
    this.lastChain = chain;
    if (record === "ATOM  ") {
      const number = parseResidueNumber(line.slice(22, 26));
      if (number !== null) {
        const span = this.atomRange.get(chain) ?? { first: number, last: number };
        this.atomRange.set(chain, { first: Math.min(span.first, number), last: Math.max(span.last, number) });
      }
      return;
    }
    const key = instanceKey(instance);
    if (!this.firstLine.has(key)) {
      this.firstLine.set(key, lineNumber);
    }
  }

  /** A LINK record: two residues joined by a covalent or metal coordination
   *  bond. Identical pairs are kept once. */
  addLink(line: string): void {
    const first: CompoundInstance = {
      compound: line.slice(17, 20).trim().toUpperCase(),
      asym: line.slice(21, 22).trim(),
      seq: line.slice(22, 26).trim(),
      ins: line.slice(26, 27).trim(),
    };
    const second: CompoundInstance = {
      compound: line.slice(47, 50).trim().toUpperCase(),
      asym: line.slice(51, 52).trim(),
      seq: line.slice(52, 56).trim(),
      ins: line.slice(56, 57).trim(),
    };
    const key = `${instanceKey(first)}\u0001${instanceKey(second)}`;
    if (!this.linkSeen.has(key)) {
      this.linkSeen.add(key);
      this.linkPairs.push([first, second]);
    }
  }

  /** The residue names of every chain's SEQRES records. */
  sequenceNames(): Set<string> {
    const names = new Set<string>();
    for (const chainNames of this.seqres.values()) {
      for (const name of chainNames) {
        names.add(name);
      }
    }
    return names;
  }

  /** The LINK records as the metal rule reads metalc rows. A file with a LINK
   *  record that involves a metal ion is treated like an mmCIF file with metalc
   *  rows; LINK records between other residues do not count. */
  metalcLinks(isMetal: (compound: string) => boolean): MetalcLinks {
    return {
      hasRows: this.linkPairs.some(([first, second]) => isMetal(first.compound) || isMetal(second.compound)),
      pairs: this.linkPairs,
    };
  }

  /** A residue MODRES lists is a member. Otherwise it must sit in the chain's
   *  polymer block (before the chain's TER record, or, for a chain without one
   *  or a residue the coordinates did not reach, within the residue numbers of
   *  the chain's ATOM records). In the block, it is a member when its name is in
   *  the chain's SEQRES, and only "within the span" when the chain has none. */
  position(instance: CompoundInstance): ChainPosition {
    if (this.modified.has(instanceKey(instance))) {
      return "member";
    }
    const names = this.seqres.get(instance.asym);
    if (!this.inPolymerBlock(instance)) {
      return "none";
    }
    if (!names) {
      return "spanOnly";
    }
    return names.has(instance.compound) ? "member" : "none";
  }

  private inPolymerBlock(instance: CompoundInstance): boolean {
    const terLine = this.terLine.get(instance.asym);
    const firstLine = this.firstLine.get(instanceKey(instance));
    if (terLine !== undefined && firstLine !== undefined) {
      return firstLine < terLine;
    }
    const span = this.atomRange.get(instance.asym);
    const number = parseResidueNumber(instance.seq);
    return span !== undefined && number !== null && number >= span.first && number <= span.last;
  }
}

/** A residue number as Go's strconv.Atoi reads it: digits with an optional
 *  sign, nothing else. */
function parseResidueNumber(raw: string): number | null {
  const text = raw.trim();
  return /^[+-]?[0-9]+$/.test(text) ? Number.parseInt(text, 10) : null;
}
