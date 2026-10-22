import { cifLoop, cifValue, loopValue, type CifLoop } from "./cif";
import { CIF_REFERENCE } from "./cifReference.gen";

// What a coordinate file says about itself.
//
// A deposited model is not just coordinates: PDB REMARK 3 and the mmCIF
// `_software` / `_refine` categories record which program refined it, at what
// version, and how well it fit the data. Reading that is the difference
// between asking the depositor to retype what they already uploaded and simply
// showing them what we found.

export type StructureFacts = {
  program: { name: string; version: string | null } | null;
  /**
   * Depositors, in the `Surname, I.N.` form the app displays. Taken from
   * mmCIF `_audit_author` or PDB `AUTHOR` — the people who deposited the
   * structure, not `_citation_author`, who wrote the paper about it.
   */
  authors: string[];
  /** Four-character PDB accession, when the file admits to having one. */
  pdbId: string | null;
  metrics: {
    r_work?: number;
    r_free?: number;
  };
  metadata: {
    resolution?: number;
    space_group?: string;
    /** One of the backend's StructureMethod values, or absent. The header
     *  writes "X-RAY DIFFRACTION"; the record stores "X-ray crystallography",
     *  and a value outside the enum would be saved verbatim and never match a
     *  filter, so an unrecognised method is dropped rather than guessed. */
    method?: string;
    atom_count?: number;
    modeled_residues?: number;
    unique_protein_chains?: number;
    altloc_fraction?: number;
    ligands?: string[];
    /** Organic cofactors (heme, NAD, ...) and metal ions, sorted with the same
     *  lists as the backend parser (lib/cif/cofactors.json). */
    cofactors?: string[];
    /** Sent on the model's open `extensions` object, like the CLI does: the
     *  compounds the parser kept but is unsure about, for a person to confirm. */
    extensions?: { compound_review: CompoundReview[] };
  };
  /** Compounds the preview kept but is unsure about. Nothing is dropped on
   *  this account. The same list is `metadata.extensions.compound_review`. */
  needsReview: CompoundReview[];
};

/** One compound flagged for review: the shape of the backend's
 *  `extensions.compound_review` entries. */
export type CompoundReview = {
  list: "ligands" | "cofactors";
  id: string;
  reason: string;
};

export type StructureFormat = "pdb" | "mmcif";

// Water is always dropped: every crystal has it and nobody means it when they
// say "the ligand".
const WATER_COMPOUNDS = new Set(["HOH", "DOD", "WAT", "H2O"]);

// The reference lists are generated from lib/cif/cofactors.json and
// lib/cif/solvents.json (npm run gen:cif-reference), so the preview and the
// backend parser cannot disagree about them. While either list is a draft
// (`approved: false` in the JSON), only water is dropped; other listed solvents
// are kept and flagged for review.
const REFERENCE_LISTS_APPROVED: boolean = CIF_REFERENCE.approved;
const SOLVENT_COMPOUNDS = new Set<string>(CIF_REFERENCE.solvents);
const COFACTOR_COMPOUNDS = new Set<string>(CIF_REFERENCE.cofactors);
const METAL_IONS = new Set<string>(CIF_REFERENCE.metalIons);

type CompoundKind = "ligand" | "cofactor" | "metal" | "solvent" | "draftSolvent";

// Mirrors Reference.kind in lib/cif/reference.go.
function compoundKind(compound: string): CompoundKind {
  if (WATER_COMPOUNDS.has(compound)) {
    return "solvent";
  }
  if (SOLVENT_COMPOUNDS.has(compound)) {
    return REFERENCE_LISTS_APPROVED ? "solvent" : "draftSolvent";
  }
  if (METAL_IONS.has(compound)) {
    return "metal";
  }
  if (COFACTOR_COMPOUNDS.has(compound)) {
    return "cofactor";
  }
  return "ligand";
}

export function detectStructureFormat(name: string): StructureFormat | null {
  const lower = name.toLowerCase();
  if (lower.endsWith(".pdb") || lower.endsWith(".ent")) {
    return "pdb";
  }
  if (lower.endsWith(".cif") || lower.endsWith(".mmcif") || lower.endsWith(".bcif")) {
    return "mmcif";
  }
  return null;
}

export function parseStructureFacts(
  text: string,
  format: StructureFormat,
): StructureFacts {
  return format === "pdb" ? parsePdb(text) : parseMmcif(text);
}

/* ------------------------------------------------------------------ PDB -- */

function parsePdb(text: string): StructureFacts {
  const lines = text.split("\n");
  const facts: StructureFacts = {
    program: null,
    authors: [],
    pdbId: null,
    metrics: {},
    metadata: {},
    needsReview: [],
  };
  const authorLines: string[] = [];

  let atomCount = 0;
  const residueTally = new ResidueTally();
  const compounds = new CompoundTally();
  // Only the first model counts, as in lib/cif.
  let modelStarted = false;
  let firstModelDone = false;

  for (const line of lines) {
    const record = line.slice(0, 6);

    if (record === "HET   ") {
      const compound = line.slice(7, 10).trim().toUpperCase();
      if (compound) {
        compounds.declare(
          {
            compound,
            asym: line.slice(12, 13).trim(),
            seq: line.slice(13, 17).trim(),
            ins: line.slice(17, 18).trim(),
          },
          Number.parseInt(line.slice(20, 25).trim(), 10) || 0,
        );
      }
      continue;
    }
    if (record === "MODEL ") {
      if (modelStarted) {
        firstModelDone = true;
      }
      modelStarted = true;
      continue;
    }
    if (record === "ENDMDL") {
      firstModelDone = true;
      continue;
    }

    if (record === "ATOM  " || record === "HETATM") {
      if (firstModelDone) {
        continue;
      }
      const compound = line.slice(17, 20).trim().toUpperCase();
      const chain = line.slice(21, 22).trim();
      if (record === "HETATM" && compound) {
        compounds.addAtom(
          {
            compound,
            asym: chain,
            seq: line.slice(22, 26).trim(),
            ins: line.slice(26, 27).trim(),
          },
          line.slice(12, 16).trim(),
        );
      }
      if (record === "HETATM" && WATER_COMPOUNDS.has(compound)) {
        continue;
      }
      // Hydrogens are not modelled at this resolution and are excluded from
      // the count deposited alongside published structures.
      const element = line.slice(76, 78).trim().toUpperCase();
      if (element === "H" || element === "D") {
        continue;
      }
      atomCount += 1;
      if (record === "ATOM  ") {
        const occupancy = Number.parseFloat(line.slice(54, 60).trim());
        residueTally.add(
          chain,
          `${chain}|${line.slice(22, 26).trim()}|${line.slice(26, 27).trim()}`,
          line.slice(16, 17).trim(),
          Number.isFinite(occupancy) ? occupancy : 1,
        );
      }
      continue;
    }

    if (record === "CRYST1") {
      const group = line.slice(55, 66).trim();
      if (group) {
        facts.metadata.space_group = group;
      }
      continue;
    }

    if (record === "AUTHOR") {
      authorLines.push(line.slice(10).trim());
      continue;
    }

    if (record === "HEADER") {
      // Columns 63-66 hold the accession.
      facts.pdbId = normalizePdbId(line.slice(62, 66));
      continue;
    }

    if (record === "EXPDTA") {
      const method = canonicalMethod(line.slice(10).trim());
      if (method) {
        facts.metadata.method = method;
      }
      continue;
    }

    if (record !== "REMARK") {
      continue;
    }

    // REMARK 3 carries the refinement record; REMARK 200 the data collection.
    const body = line.slice(11);
    if (!facts.program) {
      const program = /^\s*PROGRAM\s*:\s*(.+?)\s*$/i.exec(body);
      if (program && !/NULL|NONE/i.test(program[1])) {
        facts.program = splitProgram(program[1]);
      }
    }
    readNumber(body, /^\s*R VALUE\s+\(WORKING SET\)\s*:\s*([0-9.]+)/i, (value) => {
      facts.metrics.r_work = value;
    });
    readNumber(body, /^\s*FREE R VALUE\s*:\s*([0-9.]+)/i, (value) => {
      facts.metrics.r_free = value;
    });
    readNumber(
      body,
      /RESOLUTION RANGE HIGH\s*\(A(?:NGSTROMS)?\)\s*:\s*([0-9.]+)/i,
      (value) => {
        if (facts.metadata.resolution === undefined) {
          facts.metadata.resolution = value;
        }
      },
    );
  }

  facts.authors = authorLines
    .join(",")
    .split(",")
    .map((name) => name.trim())
    .filter(Boolean)
    .map(pdbAuthorName);

  applyCounts(facts, atomCount, residueTally, compounds, NO_METALC_ROWS);
  return facts;
}

/**
 * `A.ATTIGANI` and `S.P.LI` become `Attigani, A.` and `Li, S.P.` — the form
 * mmCIF uses and the entry pages display. The last dot separates initials
 * from the surname.
 *
 * The PDB format shouts every name, so the surname has to be re-cased on the
 * way out, and casing inside a name is unrecoverable: `A.MCDONALD` comes back
 * as `Mcdonald, A.`. mmCIF, where the entry has one, is preferred for exactly
 * this reason.
 */
function pdbAuthorName(raw: string): string {
  const lastDot = raw.lastIndexOf(".");
  if (lastDot === -1 || lastDot === raw.length - 1) {
    return titleCase(raw);
  }
  const initials = raw.slice(0, lastDot + 1).replace(/\s+/g, "");
  const surname = titleCase(raw.slice(lastDot + 1).trim());
  return surname ? `${surname}, ${initials}` : titleCase(raw);
}

function splitProgram(raw: string): { name: string; version: string | null } {
  // "PHENIX (1.21.2_5108)" and "REFMAC 5.8.0258" are both common.
  const parenthesised = /^(.+?)\s*\(([^)]+)\)\s*$/.exec(raw.trim());
  if (parenthesised) {
    return {
      name: parenthesised[1].trim(),
      version: cleanVersion(parenthesised[2]),
    };
  }
  const trailing = /^(.+?)\s+v?([0-9][0-9A-Za-z._-]*)$/.exec(raw.trim());
  if (trailing) {
    return { name: trailing[1].trim(), version: cleanVersion(trailing[2]) };
  }
  return { name: raw.trim(), version: null };
}

/**
 * phenix writes the release and the build tag together: `2.0_5824: ???` when
 * it does not know its own revision, `1.21.2_5108: 0e26e8b` when it does. The
 * release is the version anyone quotes; the tag after the colon is noise, and
 * `???` is worse than noise.
 */
function cleanVersion(raw: string): string | null {
  const release = raw.split(":")[0].trim();
  return release && !/^\?+$/.test(release) ? release : null;
}

function readNumber(
  line: string,
  pattern: RegExp,
  assign: (value: number) => void,
): void {
  const match = pattern.exec(line);
  if (!match) {
    return;
  }
  const value = Number.parseFloat(match[1]);
  if (Number.isFinite(value)) {
    assign(value);
  }
}

/* --------------------------------------------------------------- mmCIF -- */

function parseMmcif(text: string): StructureFacts {
  const facts: StructureFacts = {
    program: null,
    authors: [],
    pdbId: null,
    metrics: {},
    metadata: {},
    needsReview: [],
  };

  facts.program = mmcifProgram(text);
  facts.authors = mmcifAuthors(text);
  facts.pdbId =
    normalizePdbId(cifValue(text, "_entry.id") ?? "") ??
    normalizePdbId(/^data_(\S+)/m.exec(text)?.[1] ?? "");

  const rWork = numberOf(cifValue(text, "_refine.ls_r_factor_r_work"));
  const rFree = numberOf(cifValue(text, "_refine.ls_r_factor_r_free"));
  if (rWork !== null) {
    facts.metrics.r_work = rWork;
  }
  if (rFree !== null) {
    facts.metrics.r_free = rFree;
  }

  const resolution =
    numberOf(cifValue(text, "_refine.ls_d_res_high")) ??
    numberOf(cifValue(text, "_reflns.d_resolution_high"));
  if (resolution !== null) {
    facts.metadata.resolution = resolution;
  }

  const spaceGroup =
    cifValue(text, "_symmetry.space_group_name_h-m") ??
    cifValue(text, "_space_group.name_h-m_alt");
  if (spaceGroup) {
    facts.metadata.space_group = spaceGroup;
  }

  const method = canonicalMethod(cifValue(text, "_exptl.method") ?? "");
  if (method) {
    facts.metadata.method = method;
  }

  const atomSite = cifLoop(text, "_atom_site");
  if (atomSite) {
    let atomCount = 0;
    const residueTally = new ResidueTally();
    const compounds = new CompoundTally();
    // Only the first model counts, as in lib/cif.
    let firstModel: string | null | undefined;

    for (const row of atomSite.rows) {
      const model = loopValue(atomSite, row, "pdbx_pdb_model_num");
      if (firstModel === undefined) {
        firstModel = model;
      }
      if (model !== firstModel) {
        continue;
      }
      const group = (loopValue(atomSite, row, "group_pdb") ?? "ATOM").toUpperCase();
      const compound = (loopValue(atomSite, row, "label_comp_id") ?? "").toUpperCase();
      const element = (loopValue(atomSite, row, "type_symbol") ?? "").toUpperCase();
      if (group === "HETATM" && compound) {
        compounds.addAtom(
          {
            compound,
            asym: loopValue(atomSite, row, "label_asym_id") ?? "",
            seq: loopValue(atomSite, row, "auth_seq_id") ?? "",
            ins: loopValue(atomSite, row, "pdbx_pdb_ins_code") ?? "",
          },
          loopValue(atomSite, row, "label_atom_id") ?? "",
        );
      }
      if (group === "HETATM" && WATER_COMPOUNDS.has(compound)) {
        continue;
      }
      if (element === "H" || element === "D") {
        continue;
      }
      atomCount += 1;
      if (group === "HETATM") {
        continue;
      }
      const chain =
        loopValue(atomSite, row, "auth_asym_id") ??
        loopValue(atomSite, row, "label_asym_id") ??
        "";
      const seq =
        loopValue(atomSite, row, "auth_seq_id") ??
        loopValue(atomSite, row, "label_seq_id") ??
        "";
      const occupancy = Number.parseFloat(loopValue(atomSite, row, "occupancy") ?? "");
      residueTally.add(
        chain,
        `${chain}|${seq}|${loopValue(atomSite, row, "pdbx_pdb_ins_code") ?? ""}`,
        loopValue(atomSite, row, "label_alt_id") ?? "",
        Number.isFinite(occupancy) ? occupancy : 1,
      );
    }
    for (const compound of nonpolymerCompounds(text)) {
      compounds.declare({ compound, asym: "", seq: "", ins: "" }, 0);
    }
    applyCounts(facts, atomCount, residueTally, compounds, readMetalcLinks(text));
  }

  return facts;
}

/** Compounds `_pdbx_entity_nonpoly` lists, which may have no coordinates. */
function nonpolymerCompounds(text: string): string[] {
  const loop = cifLoop(text, "_pdbx_entity_nonpoly");
  if (loop) {
    return loop.rows
      .map((row) => (loopValue(loop, row, "comp_id") ?? "").toUpperCase())
      .filter(Boolean);
  }
  const single = (cifValue(text, "_pdbx_entity_nonpoly.comp_id") ?? "").toUpperCase();
  return single ? [single] : [];
}

function mmcifAuthors(text: string): string[] {
  const loop = cifLoop(text, "_audit_author");
  if (loop) {
    return loop.rows
      .map((row) => loopValue(loop, row, "name"))
      .filter((name): name is string => Boolean(name));
  }
  const single = cifValue(text, "_audit_author.name");
  return single ? [single] : [];
}

function mmcifProgram(text: string): { name: string; version: string | null } | null {
  const software = cifLoop(text, "_software");
  if (software) {
    // Prefer the entry classified as refinement; a deposition lists half a
    // dozen programs and only one of them made these coordinates.
    const rows = software.rows;
    const refinement = rows.find((row) =>
      /refinement/i.test(loopValue(software, row, "classification") ?? ""),
    );
    const chosen = refinement ?? rows[rows.length - 1];
    if (chosen) {
      const name = loopValue(software, chosen, "name");
      if (name) {
        return { name, version: loopValue(software, chosen, "version") };
      }
    }
  }
  const single = cifValue(text, "_software.name");
  if (single) {
    return { name: single, version: cifValue(text, "_software.version") };
  }
  const computing = cifValue(text, "_computing.structure_refinement");
  return computing ? splitProgram(computing) : null;
}

function numberOf(value: string | null): number | null {
  if (value === null) {
    return null;
  }
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : null;
}

/* -------------------------------------------------------------- shared -- */

/** One placed copy of a non-polymer compound. Mirrors compoundInstance in
 *  lib/cif/extract.go: an instance read from `_pdbx_entity_nonpoly` alone has
 *  no asym ID and no atoms. */
type CompoundInstance = {
  compound: string;
  asym: string;
  seq: string;
  ins: string;
};

function instanceKey(instance: CompoundInstance): string {
  return [instance.compound, instance.asym, instance.seq, instance.ins].join("\u0000");
}

/** The coordinates' view of the non-polymer compounds: the distinct atoms seen
 *  per instance, and the atom count a PDB `HET` record declares. */
class CompoundTally {
  readonly instances = new Map<string, { instance: CompoundInstance; atoms: Set<string> }>();
  readonly declared = new Map<string, { instance: CompoundInstance; count: number }>();

  addAtom(instance: CompoundInstance, atomName: string): void {
    const key = instanceKey(instance);
    const entry = this.instances.get(key) ?? { instance, atoms: new Set<string>() };
    entry.atoms.add(atomName);
    this.instances.set(key, entry);
  }

  declare(instance: CompoundInstance, count: number): void {
    this.declared.set(instanceKey(instance), { instance, count });
  }

  /** Instance → atom count, 0 when unknown. A declared count wins. */
  counts(): Map<string, { instance: CompoundInstance; atomCount: number }> {
    const counts = new Map<string, { instance: CompoundInstance; atomCount: number }>();
    for (const [key, entry] of this.instances) {
      counts.set(key, { instance: entry.instance, atomCount: entry.atoms.size });
    }
    for (const [key, entry] of this.declared) {
      if (entry.count > 0) {
        counts.set(key, { instance: entry.instance, atomCount: entry.count });
      } else if (!counts.has(key)) {
        counts.set(key, { instance: entry.instance, atomCount: 0 });
      }
    }
    return counts;
  }
}

/** Residues and alternate locations, as lib/cif counts them: a residue is
 *  altloc when two alternate positions each reach this occupancy. */
const MIN_ALTLOC_OCCUPANCY = 0.1;

class ResidueTally {
  readonly residues = new Set<string>();
  readonly chains = new Set<string>();
  private readonly positions = new Map<string, Map<string, number>>();

  add(chain: string, residue: string, altID: string, occupancy: number): void {
    this.residues.add(residue);
    if (chain) {
      this.chains.add(chain);
    }
    if (altID) {
      const byAltID = this.positions.get(residue) ?? new Map<string, number>();
      byAltID.set(altID, Math.max(byAltID.get(altID) ?? 0, occupancy));
      this.positions.set(residue, byAltID);
    }
  }

  altlocResidueCount(): number {
    let count = 0;
    for (const byAltID of this.positions.values()) {
      let occupied = 0;
      for (const occupancy of byAltID.values()) {
        if (occupancy >= MIN_ALTLOC_OCCUPANCY) {
          occupied += 1;
        }
      }
      if (occupied >= 2) {
        count += 1;
      }
    }
    return count;
  }
}

function applyCounts(
  facts: StructureFacts,
  atomCount: number,
  residueTally: ResidueTally,
  compounds: CompoundTally,
  metalcLinks: MetalcLinks,
): void {
  const residueCount = residueTally.residues.size;
  if (atomCount > 0) {
    facts.metadata.atom_count = atomCount;
  }
  if (residueCount > 0) {
    facts.metadata.modeled_residues = residueCount;
  }
  if (residueTally.chains.size > 0) {
    facts.metadata.unique_protein_chains = residueTally.chains.size;
  }
  if (residueCount > 0) {
    facts.metadata.altloc_fraction = residueTally.altlocResidueCount() / residueCount;
  }
  sortCompounds(facts, compounds.counts(), metalcLinks);
}

/** What the file's `metalc` connection rows say about metal ions. */
type MetalcLinks = {
  /** False when the file has no metalc rows at all (every PDB file here). */
  hasRows: boolean;
  /** The two partners of each metalc row. */
  pairs: Array<[CompoundInstance, CompoundInstance]>;
};

const NO_METALC_ROWS: MetalcLinks = { hasRows: false, pairs: [] };

// Review reasons: the same text as lib/cif/extract.go and reference.go. The
// parity test (parity-golden.json) fails when they drift.
const DRAFT_SOLVENT_REASON =
  "compound is on the draft solvent list, which researchers have not approved: kept as a ligand instead of dropped";
const SINGLE_ATOM_REASON =
  "single-atom compound is not in the metal ion reference list: confirm whether it is a metal ion";
const NO_METALC_ROWS_REASON =
  "metal ion kept for review: the file has no metalc connection rows";
const UNLINKED_METAL_REASON =
  "metal ion instance has no metalc connection to a protein residue or ligand";

/**
 * The same rule as the backend parser (classifyCompounds in lib/cif/extract.go),
 * per placed instance. Water is always dropped; other solvents only once the
 * reference lists are approved, and until then they are kept and flagged.
 * Organic cofactors go to `cofactors`. A metal instance is a cofactor when a
 * metalc row joins it to the protein or a ligand; in a file with no metalc rows
 * its metals are cofactors flagged for review; in a file that has them, a metal
 * instance nothing is linked to stays a ligand, flagged. A compound with one
 * atom that is not on the metal list is a ligand flagged for review. Nothing
 * else is dropped.
 */
function sortCompounds(
  facts: StructureFacts,
  instances: Map<string, { instance: CompoundInstance; atomCount: number }>,
  metalcLinks: MetalcLinks,
): void {
  const linkedInstances = new Set<string>();
  const linkedCompounds = new Set<string>();
  for (const [first, second] of metalcLinks.pairs) {
    if (compoundKind(first.compound) === "metal" && second.compound !== "" && compoundKind(second.compound) !== "solvent") {
      linkedInstances.add(instanceKey(first));
      linkedCompounds.add(first.compound);
    }
    if (compoundKind(second.compound) === "metal" && first.compound !== "" && compoundKind(first.compound) !== "solvent") {
      linkedInstances.add(instanceKey(second));
      linkedCompounds.add(second.compound);
    }
  }

  // A compound seen only in _pdbx_entity_nonpoly is skipped when the
  // coordinates already placed an instance of it.
  const placed = new Set<string>();
  for (const { instance } of instances.values()) {
    if (instance.asym !== "") {
      placed.add(instance.compound);
    }
  }
  const ordered = [...instances.values()].filter(
    ({ instance }) => instance.asym !== "" || !placed.has(instance.compound),
  );
  ordered.sort((left, right) => {
    const a = left.instance;
    const b = right.instance;
    return (
      compareText(a.compound, b.compound) ||
      compareText(a.asym, b.asym) ||
      compareText(a.seq, b.seq) ||
      compareText(a.ins, b.ins)
    );
  });

  const ligands = new CompoundList("ligands");
  const cofactors = new CompoundList("cofactors");
  for (const { instance, atomCount } of ordered) {
    const id = instance.compound;
    switch (compoundKind(id)) {
      case "solvent":
        break;
      case "draftSolvent":
        ligands.add(id, DRAFT_SOLVENT_REASON);
        break;
      case "cofactor":
        cofactors.add(id, null);
        break;
      case "ligand":
        ligands.add(id, atomCount === 1 ? SINGLE_ATOM_REASON : null);
        break;
      case "metal": {
        const linked =
          instance.asym !== ""
            ? linkedInstances.has(instanceKey(instance))
            : linkedCompounds.has(id);
        if (linked) {
          cofactors.add(id, null);
        } else if (!metalcLinks.hasRows) {
          cofactors.add(id, NO_METALC_ROWS_REASON);
        } else {
          ligands.add(id, UNLINKED_METAL_REASON);
        }
        break;
      }
    }
  }
  if (ligands.ids().length > 0) {
    facts.metadata.ligands = ligands.ids();
  }
  if (cofactors.ids().length > 0) {
    facts.metadata.cofactors = cofactors.ids();
  }
  facts.needsReview = [...ligands.reviews(), ...cofactors.reviews()];
  if (facts.needsReview.length > 0) {
    facts.metadata.extensions = { compound_review: facts.needsReview };
  }
}

function compareText(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

/** One entry per compound ID; a flagged entry wins over an unflagged one so a
 *  review flag on any instance is never lost. */
class CompoundList {
  private readonly entries = new Map<string, string | null>();

  constructor(private readonly list: "ligands" | "cofactors") {}

  add(id: string, reviewReason: string | null): void {
    const existing = this.entries.get(id);
    if (existing === undefined || (existing === null && reviewReason !== null)) {
      this.entries.set(id, reviewReason);
    }
  }

  ids(): string[] {
    return [...this.entries.keys()];
  }

  reviews(): CompoundReview[] {
    const reviews: CompoundReview[] = [];
    for (const [id, reason] of this.entries) {
      if (reason !== null) {
        reviews.push({ list: this.list, id, reason });
      }
    }
    return reviews;
  }
}

function readMetalcLinks(text: string): MetalcLinks {
  const connections = cifLoop(text, "_struct_conn");
  if (!connections) {
    return NO_METALC_ROWS;
  }
  const links: MetalcLinks = { hasRows: false, pairs: [] };
  for (const row of connections.rows) {
    if ((loopValue(connections, row, "conn_type_id") ?? "").toLowerCase() !== "metalc") {
      continue;
    }
    links.hasRows = true;
    links.pairs.push([
      partnerInstance(connections, row, "ptnr1", "pdbx_ptnr1"),
      partnerInstance(connections, row, "ptnr2", "pdbx_ptnr2"),
    ]);
  }
  return links;
}

function partnerInstance(
  connections: CifLoop,
  row: string[],
  prefix: string,
  extraPrefix: string,
): CompoundInstance {
  return {
    compound: (loopValue(connections, row, `${prefix}_label_comp_id`) ?? "").toUpperCase(),
    asym: loopValue(connections, row, `${prefix}_label_asym_id`) ?? "",
    seq: loopValue(connections, row, `${prefix}_auth_seq_id`) ?? "",
    ins: loopValue(connections, row, `${extraPrefix}_pdb_ins_code`) ?? "",
  };
}

/**
 * Maps what the headers actually say onto the two values the record accepts.
 * Anything else returns null: storing an off-enum string would render as a
 * one-off label nobody can search for.
 */
/** The header's spelling of the method mapped onto the record's own
 *  vocabulary. Null for anything outside it: a value the record cannot hold
 *  would be saved verbatim and never match a filter. */
export function canonicalMethod(raw: string): string | null {
  const value = raw.trim().toLowerCase();
  if (!value) {
    return null;
  }
  if (value.includes("x-ray") || value.includes("xray")) {
    return "X-ray crystallography";
  }
  if (
    value.includes("electron microscopy") ||
    value.includes("cryo-em") ||
    value.includes("cryoem")
  ) {
    return "CryoEM";
  }
  return null;
}

function normalizePdbId(raw: string): string | null {
  const id = raw.trim().toUpperCase();
  return /^[1-9][A-Z0-9]{3}$/.test(id) ? id : null;
}

function titleCase(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) {
    return trimmed;
  }
  // Headers are shouted in the PDB format; anything already mixed-case came
  // from mmCIF and is left alone.
  if (trimmed !== trimmed.toUpperCase()) {
    return trimmed;
  }
  return trimmed
    .toLowerCase()
    .replace(/(^|[\s-])([a-z])/g, (_match, boundary: string, letter: string) =>
      `${boundary}${letter.toUpperCase()}`,
    );
}
