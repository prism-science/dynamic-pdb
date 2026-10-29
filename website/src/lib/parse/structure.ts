import { cifLoop, cifValue, loopColumn, loopValue, type CifLoop } from "./cif";
import { PdbChains } from "./pdbChains";
import {
  CompoundTally,
  NO_METALC_ROWS,
  WATER_COMPOUNDS,
  isMetalIon,
  pdbAnnotations,
  readAnnotations,
  sortCompounds,
  type CompoundInstance,
  type CompoundReview,
  type CompoundSources,
  type FileAnnotations,
  type Glycan,
  type InstanceFacts,
  type MetalcLinks,
} from "./ligandRule";

export type { CompoundReview, CompoundSources, Glycan } from "./ligandRule";

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
  /** The rule that placed each compound, including the ones left out, as the
   *  extract endpoint's `compound_sources` gives it. Not sent anywhere yet. */
  compoundSources: CompoundSources;
  /** One glycan per branched entity with its WURCS descriptors, as the extract
   *  endpoint's `glycan_descriptors` gives them. Not sent anywhere yet. */
  glycans: Glycan[];
};

export type StructureFormat = "pdb" | "mmcif";

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
    compoundSources: { ligands: {}, cofactors: {}, excluded: {} },
    glycans: [],
  };
  const authorLines: string[] = [];

  let atomCount = 0;
  const residueTally = new ResidueTally();
  const compounds = new CompoundTally();
  // Only the first model counts, as in lib/cif.
  let modelStarted = false;
  let firstModelDone = false;

  const chains = new PdbChains();
  for (const [index, line] of lines.entries()) {
    const lineNumber = index + 1;
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
          "",
        );
      }
      continue;
    }
    switch (record.trim()) {
      case "SEQRES":
        chains.addSeqres(line);
        continue;
      case "MODRES":
        chains.addModres(line);
        continue;
      case "LINK":
        chains.addLink(line);
        continue;
      case "TER":
        if (!firstModelDone) {
          chains.addTer(line, lineNumber);
        }
        continue;
      default:
        // Every other record is read below, or carries nothing the rule uses.
        break;
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
      const element = line.slice(76, 78).trim().toUpperCase();
      const instance = {
        compound,
        asym: chain,
        seq: line.slice(22, 26).trim(),
        ins: line.slice(26, 27).trim(),
      };
      chains.addCoordinate(record, line, lineNumber, instance);
      if (record === "HETATM" && compound) {
        compounds.addAtom(instance, {
          name: line.slice(12, 16).trim(),
          element,
          entityId: "",
          inChain: false,
          laterModel: false,
        });
      }
      if (record === "HETATM" && WATER_COMPOUNDS.has(compound)) {
        continue;
      }
      // Hydrogens are not modelled at this resolution and are excluded from
      // the count deposited alongside published structures.
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

  const annotations = pdbAnnotations();
  annotations.modifiedResidues = [...chains.modifiedNames];
  annotations.sequenceNames = chains.sequenceNames();
  const instances = compounds.facts();
  for (const instance of instances.values()) {
    instance.chain = chains.position(instance.instance);
  }
  applyCounts(facts, atomCount, residueTally, instances, chains.metalcLinks(isMetalIon), annotations);
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
    compoundSources: { ligands: {}, cofactors: {}, excluded: {} },
    glycans: [],
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
      const laterModel = model !== firstModel;
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
          {
            name: loopValue(atomSite, row, "label_atom_id") ?? "",
            element,
            entityId: loopValue(atomSite, row, "label_entity_id") ?? "",
            inChain: loopValue(atomSite, row, "label_seq_id") !== null,
            laterModel,
          },
        );
      }
      // Only the first model is counted; later models add compound instances
      // that an integrative (IHM) file lists too.
      if (laterModel) {
        continue;
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
    for (const { compound, entityId } of nonpolymerCompounds(text)) {
      compounds.declare({ compound, asym: "", seq: "", ins: "" }, 0, entityId);
    }
    const annotations = readAnnotations(text, loopColumn(atomSite, "ihm_model_id") !== -1);
    applyCounts(facts, atomCount, residueTally, compounds.facts(), readMetalcLinks(text), annotations);
    facts.glycans = annotations.glycans;
  }

  return facts;
}

/** Compounds `_pdbx_entity_nonpoly` lists, which may have no coordinates. */
function nonpolymerCompounds(text: string): Array<{ compound: string; entityId: string }> {
  const loop = cifLoop(text, "_pdbx_entity_nonpoly");
  if (loop) {
    return loop.rows
      .map((row) => ({
        compound: (loopValue(loop, row, "comp_id") ?? "").toUpperCase(),
        entityId: loopValue(loop, row, "entity_id") ?? "",
      }))
      .filter(({ compound }) => compound !== "");
  }
  const single = (cifValue(text, "_pdbx_entity_nonpoly.comp_id") ?? "").toUpperCase();
  return single ? [{ compound: single, entityId: cifValue(text, "_pdbx_entity_nonpoly.entity_id") ?? "" }] : [];
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
  instances: Map<string, InstanceFacts>,
  metalcLinks: MetalcLinks,
  annotations: FileAnnotations,
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
  const sorted = sortCompounds(instances, metalcLinks, annotations);
  if (sorted.ligands.length > 0) {
    facts.metadata.ligands = sorted.ligands;
  }
  if (sorted.cofactors.length > 0) {
    facts.metadata.cofactors = sorted.cofactors;
  }
  facts.needsReview = sorted.reviews;
  if (facts.needsReview.length > 0) {
    facts.metadata.extensions = { compound_review: facts.needsReview };
  }
  facts.compoundSources = sorted.sources;
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
