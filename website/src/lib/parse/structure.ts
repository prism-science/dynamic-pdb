import { cifLoop, cifValue, loopValue } from "./cif";

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
   *
   * Affiliation has no counterpart: released PDB and mmCIF entries do not
   * carry one, so that field stays for the depositor to fill.
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
    organism?: string;
    /** One of the backend's StructureMethod values, or absent. The header
     *  writes "X-RAY DIFFRACTION"; the record stores "X-ray crystallography",
     *  and a value outside the enum would be saved verbatim and never match a
     *  filter, so an unrecognised method is dropped rather than guessed. */
    method?: string;
    atom_count?: number;
    modeled_residues?: number;
    unique_protein_chains?: number;
    ligands?: string[];
  };
};

export type StructureFormat = "pdb" | "mmcif";

// Solvent, cryoprotectants and buffer components. Every crystal has them and
// nobody means them when they say "the ligand", so they stay out of the list
// while genuine hetero-compounds stay in.
const INCIDENTAL_COMPOUNDS = new Set([
  "HOH", "DOD", "WAT", "H2O",
  "SO4", "PO4", "NO3", "CO3", "ACT", "ACY", "FMT", "TRS", "EPE", "MES", "CIT", "TLA",
  "GOL", "EDO", "PEG", "PG4", "PGE", "1PE", "MPD", "DMS", "IPA", "MOH", "BME",
  "NA", "K", "MG", "CA", "CL", "BR", "IOD", "ZN", "MN", "FE", "FE2", "NI", "CU", "CD", "CO", "HG", "CS", "RB", "SR", "BA", "LI", "F",
]);

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
  };
  const authorLines: string[] = [];

  const atoms = { count: 0 };
  const residues = new Set<string>();
  const chains = new Set<string>();
  const ligands = new Set<string>();

  for (const line of lines) {
    const record = line.slice(0, 6);

    if (record === "ATOM  " || record === "HETATM") {
      const compound = line.slice(17, 20).trim().toUpperCase();
      if (record === "HETATM" && INCIDENTAL_COMPOUNDS.has(compound)) {
        continue;
      }
      // Hydrogens are not modelled at this resolution and are excluded from
      // the count deposited alongside published structures.
      const element = line.slice(76, 78).trim().toUpperCase();
      if (element === "H" || element === "D") {
        continue;
      }
      atoms.count += 1;
      const chain = line.slice(21, 22).trim();
      if (record === "ATOM  ") {
        residues.add(`${chain}|${line.slice(22, 27).trim()}`);
        if (chain) {
          chains.add(chain);
        }
      } else {
        ligands.add(compound);
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

    if (record === "SOURCE") {
      const match = /ORGANISM_SCIENTIFIC:\s*([^;]+)/i.exec(line);
      if (match && !facts.metadata.organism) {
        facts.metadata.organism = binomial(match[1].trim());
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

  applyCounts(facts, atoms.count, residues.size, chains.size, ligands);
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

  const organism =
    cifValue(text, "_entity_src_gen.pdbx_gene_src_scientific_name") ??
    cifValue(text, "_entity_src_nat.pdbx_organism_scientific");
  if (organism) {
    facts.metadata.organism = binomial(organism);
  }

  const atomSite = cifLoop(text, "_atom_site");
  if (atomSite) {
    let atomCount = 0;
    const residues = new Set<string>();
    const chains = new Set<string>();
    const ligands = new Set<string>();

    for (const row of atomSite.rows) {
      const group = loopValue(atomSite, row, "group_pdb") ?? "ATOM";
      const compound = (loopValue(atomSite, row, "label_comp_id") ?? "").toUpperCase();
      const element = (loopValue(atomSite, row, "type_symbol") ?? "").toUpperCase();
      if (group === "HETATM" && INCIDENTAL_COMPOUNDS.has(compound)) {
        continue;
      }
      if (element === "H" || element === "D") {
        continue;
      }
      atomCount += 1;
      const chain =
        loopValue(atomSite, row, "auth_asym_id") ??
        loopValue(atomSite, row, "label_asym_id") ??
        "";
      if (group === "HETATM") {
        ligands.add(compound);
      } else {
        const seq =
          loopValue(atomSite, row, "auth_seq_id") ??
          loopValue(atomSite, row, "label_seq_id") ??
          "";
        residues.add(`${chain}|${seq}`);
        if (chain) {
          chains.add(chain);
        }
      }
    }
    applyCounts(facts, atomCount, residues.size, chains.size, ligands);
  }

  return facts;
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

function applyCounts(
  facts: StructureFacts,
  atomCount: number,
  residueCount: number,
  chainCount: number,
  ligands: Set<string>,
): void {
  if (atomCount > 0) {
    facts.metadata.atom_count = atomCount;
  }
  if (residueCount > 0) {
    facts.metadata.modeled_residues = residueCount;
  }
  if (chainCount > 0) {
    facts.metadata.unique_protein_chains = chainCount;
  }
  if (ligands.size > 0) {
    facts.metadata.ligands = [...ligands].sort();
  }
}

/**
 * Maps what the headers actually say onto the two values the record accepts.
 * Anything else returns null: storing an off-enum string would render as a
 * one-off label nobody can search for.
 */
function canonicalMethod(raw: string): string | null {
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

/**
 * `KLEBSIELLA PNEUMONIAE` is not a title: under binomial nomenclature the
 * genus is capitalised and the species epithet never is. Title-casing the
 * whole string, as the PDB's shouting invites, produces a name no journal
 * would print.
 *
 * Anything already mixed-case came from mmCIF, which stores it correctly.
 */
function binomial(value: string): string {
  const trimmed = value.trim();
  if (!trimmed || trimmed !== trimmed.toUpperCase()) {
    return trimmed;
  }
  const lower = trimmed.toLowerCase();
  return lower.charAt(0).toUpperCase() + lower.slice(1);
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
