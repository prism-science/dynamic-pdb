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
    /** Metal ions kept as cofactors. The browser has no CCD cofactor list, so
     *  organic cofactors such as heme stay in `ligands`; the backend sorts
     *  them when it reads the uploaded file. */
    cofactors?: string[];
  };
  /** Compounds the preview kept but is unsure about; the form shows them for a
   *  person to confirm. Nothing is dropped on this account. */
  needsReview: {
    ligands: string[];
    cofactors: string[];
  };
};

export type StructureFormat = "pdb" | "mmcif";

// Water is always dropped: every crystal has it and nobody means it when they
// say "the ligand".
const WATER_COMPOUNDS = new Set(["HOH", "DOD", "WAT", "H2O"]);

// Draft list of other solvents, cryoprotectants and buffer components; mirrors
// the `solvents` list of lib/cif/solvents.json. Researchers have not approved it
// (several entries, such as citrate, sulfate and phosphate, are often real
// ligands), so until REFERENCE_LISTS_APPROVED is true these compounds are kept
// and flagged for review instead of dropped. Set it to true only together with
// `"approved": true` in both lib/cif/*.json files.
const DRAFT_SOLVENT_COMPOUNDS = new Set([
  "GOL", "EDO", "PEG", "PGE", "PG4", "1PE", "2PE", "P6G", "PE4", "MPD", "MRD", "DMS",
  "ACT", "ACY", "FMT", "TRS", "EPE", "MES", "BME", "TAM", "BTB", "IMD", "CIT",
  "SO4", "PO4", "NO3", "NH4", "SCN", "CL", "BR", "IOD",
]);
const REFERENCE_LISTS_APPROVED = false;

function isDroppedCompound(compound: string): boolean {
  return (
    WATER_COMPOUNDS.has(compound) ||
    (REFERENCE_LISTS_APPROVED && DRAFT_SOLVENT_COMPOUNDS.has(compound))
  );
}

function isDraftSolvent(compound: string): boolean {
  return !REFERENCE_LISTS_APPROVED && DRAFT_SOLVENT_COMPOUNDS.has(compound);
}

// Single-atom metal ions. Catalytic and structural metals are real findings,
// so they are kept; mirrors `metal_ions` of lib/cif/cofactors.json.
const METAL_IONS = new Set([
  "AG", "AL", "AU", "AU3", "BA", "CA", "CD", "CO", "3CO", "CR", "CS", "CU", "CU1", "CU3",
  "EU", "EU3", "FE", "FE2", "GA", "GD", "GD3", "HG", "K", "LA", "LI", "MG", "MN", "MN3",
  "MO", "NA", "NI", "3NI", "PB", "PT", "RB", "SR", "TB", "TL", "V", "W", "YB", "ZN",
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
    needsReview: { ligands: [], cofactors: [] },
  };
  const authorLines: string[] = [];

  const atoms = { count: 0 };
  const residues = new Set<string>();
  const altLocResidues = new Set<string>();
  const chains = new Set<string>();
  const ligands = new Set<string>();

  for (const line of lines) {
    const record = line.slice(0, 6);

    if (record === "ATOM  " || record === "HETATM") {
      const compound = line.slice(17, 20).trim().toUpperCase();
      if (record === "HETATM" && WATER_COMPOUNDS.has(compound)) {
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
        const residue = `${chain}|${line.slice(22, 27).trim()}`;
        residues.add(residue);
        if (line.slice(16, 17).trim()) {
          altLocResidues.add(residue);
        }
        if (chain) {
          chains.add(chain);
        }
      } else if (!isDroppedCompound(compound)) {
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

  applyCounts(
    facts,
    atoms.count,
    residues.size,
    chains.size,
    altLocResidues.size,
    ligands,
    NO_METALC_ROWS,
  );
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
    needsReview: { ligands: [], cofactors: [] },
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
    const residues = new Set<string>();
    const altLocResidues = new Set<string>();
    const chains = new Set<string>();
    const ligands = new Set<string>();

    for (const row of atomSite.rows) {
      const group = loopValue(atomSite, row, "group_pdb") ?? "ATOM";
      const compound = (loopValue(atomSite, row, "label_comp_id") ?? "").toUpperCase();
      const element = (loopValue(atomSite, row, "type_symbol") ?? "").toUpperCase();
      if (group === "HETATM" && WATER_COMPOUNDS.has(compound)) {
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
        if (!isDroppedCompound(compound)) {
          ligands.add(compound);
        }
      } else {
        const seq =
          loopValue(atomSite, row, "auth_seq_id") ??
          loopValue(atomSite, row, "label_seq_id") ??
          "";
        const residue = `${chain}|${seq}`;
        residues.add(residue);
        const altID = loopValue(atomSite, row, "label_alt_id") ?? "";
        if (altID && altID !== "." && altID !== "?") {
          altLocResidues.add(residue);
        }
        if (chain) {
          chains.add(chain);
        }
      }
    }
    applyCounts(
      facts,
      atomCount,
      residues.size,
      chains.size,
      altLocResidues.size,
      ligands,
      readMetalcLinks(text),
    );
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
  altLocResidueCount: number,
  compounds: Set<string>,
  metalcLinks: MetalcLinks,
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
  if (residueCount > 0) {
    facts.metadata.altloc_fraction = altLocResidueCount / residueCount;
  }
  sortCompounds(facts, compounds, metalcLinks);
}

/** What the file's `metalc` connection rows say about metal ions. */
type MetalcLinks = {
  /** False when the file has no metalc rows at all (every PDB file here). */
  hasRows: boolean;
  /** Metal compounds joined by a metalc row to a protein residue or ligand. */
  linked: Set<string>;
};

const NO_METALC_ROWS: MetalcLinks = { hasRows: false, linked: new Set() };

/**
 * The same rule as the backend parser: a metal ion is kept as a cofactor when a
 * metalc row links it to the protein or a ligand; in a file with no metalc rows
 * its metals are kept and flagged for review; in a file that has them, a metal
 * nothing is linked to stays a ligand, flagged. Only water is dropped while the
 * solvent list is a draft; other listed solvents are kept and flagged.
 */
function sortCompounds(
  facts: StructureFacts,
  compounds: Set<string>,
  metalcLinks: MetalcLinks,
): void {
  const ligands: string[] = [];
  const cofactors: string[] = [];
  for (const compound of [...compounds].sort()) {
    if (!METAL_IONS.has(compound)) {
      ligands.push(compound);
      if (isDraftSolvent(compound)) {
        facts.needsReview.ligands.push(compound);
      }
    } else if (metalcLinks.linked.has(compound)) {
      cofactors.push(compound);
    } else if (!metalcLinks.hasRows) {
      cofactors.push(compound);
      facts.needsReview.cofactors.push(compound);
    } else {
      ligands.push(compound);
      facts.needsReview.ligands.push(compound);
    }
  }
  if (ligands.length > 0) {
    facts.metadata.ligands = ligands;
  }
  if (cofactors.length > 0) {
    facts.metadata.cofactors = cofactors;
  }
}

function readMetalcLinks(text: string): MetalcLinks {
  const connections = cifLoop(text, "_struct_conn");
  if (!connections) {
    return NO_METALC_ROWS;
  }
  const links: MetalcLinks = { hasRows: false, linked: new Set() };
  for (const row of connections.rows) {
    if ((loopValue(connections, row, "conn_type_id") ?? "").toLowerCase() !== "metalc") {
      continue;
    }
    links.hasRows = true;
    const first = (loopValue(connections, row, "ptnr1_label_comp_id") ?? "").toUpperCase();
    const second = (loopValue(connections, row, "ptnr2_label_comp_id") ?? "").toUpperCase();
    if (first && second) {
      if (METAL_IONS.has(first) && !isDroppedCompound(second)) {
        links.linked.add(first);
      }
      if (METAL_IONS.has(second) && !isDroppedCompound(first)) {
        links.linked.add(second);
      }
    }
  }
  return links;
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
