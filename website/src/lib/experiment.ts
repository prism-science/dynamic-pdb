import type { Entry } from "@/lib/api/entries";
import { type CrystallographyView, formatKelvin, formatPH } from "@/lib/crystallography";
import { cifLoop, cifValue, loopValue } from "@/lib/parse/cif";
import { canonicalMethod } from "@/lib/parse/structure";

/**
 * What the experiment was, read out of the model's own coordinate file.
 *
 * The entry record holds four facts about the experiment -- method, resolution,
 * space group, and the pH and temperatures of its crystals. Everything else a
 * reader asks for is in the deposited mmCIF and nowhere else in our data: the
 * unit cell, the beamline, the detector, the data-collection statistics, the
 * refinement statistics, the software. So this reads the file, the way the
 * Sequence tab reads residues out of it, and prefers the record where the two
 * overlap -- the record is curated, the file is whatever the depositor wrote.
 *
 * Refinement and software belong to the selected model rather than to the
 * entry, and change with the rail: two models of one crystal share a cell and
 * differ in exactly those numbers.
 */
export type ExperimentFact = {
  label: string;
  value: string;
  /** Numbers and symmetry symbols are glyph-sensitive (P 21 21 21, 1.85 Å). */
  mono?: boolean;
  /** Free text from the depositor, which needs room to wrap. */
  wrap?: boolean;
};

/** A label/value table: most of the record is one of these. */
export type ExperimentFacts = {
  kind: "facts";
  key: string;
  title: string;
  facts: ExperimentFact[];
};

/** The data-collection statistics: overall against the highest-resolution
 *  shell. RCSB prints these as two tables; side by side is what they are
 *  quoted for, so they share one. */
export type ExperimentColumns = {
  kind: "columns";
  key: string;
  title: string;
  columns: [string, string];
  rows: ExperimentStatistic[];
};

/** A list with a head of its own: the software, the restraint deviations, the
 *  atom counts. */
export type ExperimentRows = {
  kind: "rows";
  key: string;
  title: string;
  head: string[];
  /** Which columns hold numbers, and so are set in figures against the right
   *  edge. Indexed with `head`; the first column names the row and is always
   *  text. */
  numeric: boolean[];
  rows: { key: string; cells: (string | null)[] }[];
};

/** The entry's own crystals and datasets, when it records more than one. */
export type ExperimentDatasets = {
  kind: "datasets";
  key: string;
  title: string;
  view: CrystallographyView;
};

export type ExperimentBlock =
  | ExperimentFacts
  | ExperimentColumns
  | ExperimentRows
  | ExperimentDatasets;

/** One statistic, overall and in the highest-resolution shell -- the two
 *  columns wwPDB reports them in. */
export type ExperimentStatistic = {
  key: string;
  label: string;
  overall: string | null;
  shell: string | null;
};

export type ExperimentView = {
  /** The whole tab, in order. The order is RCSB's own -- crystal, then
   *  diffraction, then what was refined from it -- and so are the headings, so
   *  that a reader who knows their page can find the same table on ours. */
  blocks: ExperimentBlock[];
};

/**
 * Whether the entry alone knows anything about the experiment.
 *
 * The tab has to be offered before the coordinate file is read -- the tab
 * strip is built on every render and the file is fetched only when the reader
 * is actually on the tab -- so the offer is made on what the record holds and
 * on whether there is a file to read at all.
 */
export function hasExperimentRecord(entry: Entry): boolean {
  return (
    trimmed(entry.method) !== null ||
    finite(entry.resolution) !== null ||
    trimmed(entry.space_group) !== null
  );
}

export function experimentView(
  entry: Entry,
  crystallography: CrystallographyView | null,
  /** The selected model's coordinate file; null when there is none to read. */
  cif: string | null,
): ExperimentView | null {
  const text = cif ?? "";
  const dataset = crystallography?.datasets[0] ?? null;

  const blocks = [
    // RCSB shows this one on the structure page rather than here, but it is
    // the five numbers a reader comes for and nothing else on our pages
    // carries them.
    facts("snapshot", "Experimental data snapshot", [
      fact("Method", method(entry, text)),
      fact("Resolution", angstrom(resolution(entry, text)), { mono: true }),
      fact("R-value free", rValue(number(text, "_refine.ls_r_factor_r_free")), { mono: true }),
      fact("R-value work", rValue(number(text, "_refine.ls_r_factor_r_work")), { mono: true }),
      fact("R-value observed", rValue(number(text, "_refine.ls_r_factor_obs")), { mono: true }),
    ]),

    facts("crystallization", "Crystallization", [
      fact("Method", sentence(cifValue(text, "_exptl_crystal_grow.method"))),
      // The record's pH first: it is the curated value, and a deposit whose
      // file says nothing often still has one.
      fact(
        "pH",
        dataset?.ph != null
          ? formatPH(dataset.ph)
          : phValue(number(text, "_exptl_crystal_grow.ph")),
        { mono: true },
      ),
      fact(
        "Temperature",
        dataset?.growthKelvin != null
          ? formatKelvin(dataset.growthKelvin)
          : kelvin(number(text, "_exptl_crystal_grow.temp")),
        { mono: true },
      ),
      fact("Details", cifValue(text, "_exptl_crystal_grow.pdbx_details"), { wrap: true }),
    ]),

    facts("properties", "Crystal properties", [
      fact(
        "Matthews coefficient",
        unit(number(text, "_exptl_crystal.density_matthews"), 2, "Å³/Da"),
        { mono: true },
      ),
      fact(
        "Solvent content",
        percent(number(text, "_exptl_crystal.density_percent_sol")),
        { mono: true },
      ),
    ]),

    facts("cell", "Crystal data", [
      fact("Space group", spaceGroup(entry, text), { mono: true }),
      fact("Unit cell lengths", lengths(text), { mono: true }),
      fact("Unit cell angles", angles(text), { mono: true }),
      fact("Z", count(number(text, "_cell.z_pdb")), { mono: true }),
    ]),

    facts("diffraction", "Diffraction experiment", [
      fact("Scattering type", sentence(cifValue(text, "_diffrn_radiation.pdbx_scattering_type"))),
      fact(
        "Collection temperature",
        dataset?.collectionKelvin != null
          ? formatKelvin(dataset.collectionKelvin)
          : kelvin(number(text, "_diffrn.ambient_temp")),
        { mono: true },
      ),
      fact("Detector", sentence(cifValue(text, "_diffrn_detector.detector"))),
      fact("Detector type", cifValue(text, "_diffrn_detector.type")),
      fact("Collection date", date(cifValue(text, "_diffrn_detector.pdbx_collection_date"))),
      fact("Monochromator", monochromator(text)),
      fact("Protocol", sentence(cifValue(text, "_diffrn_radiation.pdbx_diffrn_protocol"))),
      fact("Details", cifValue(text, "_diffrn.details"), { wrap: true }),
    ]),

    facts("source", "Radiation source", [
      fact("Source", sentence(cifValue(text, "_diffrn_source.source"))),
      fact("Type", cifValue(text, "_diffrn_source.type")),
      fact("Wavelength", angstrom(wavelength(text), 3), { mono: true }),
      fact("Synchrotron site", cifValue(text, "_diffrn_source.pdbx_synchrotron_site")),
      fact("Beamline", cifValue(text, "_diffrn_source.pdbx_synchrotron_beamline")),
    ]),

    columns(text),

    facts("refinement", "Refinement statistics", [
      fact(
        "Structure solution method",
        sentence(cifValue(text, "_refine.pdbx_method_to_determine_struct")),
      ),
      fact(
        "Cross-validation method",
        sentence(cifValue(text, "_refine.pdbx_ls_cross_valid_method")),
      ),
      fact("Starting model", cifValue(text, "_refine.pdbx_starting_model")),
      fact(
        "Resolution range",
        range(
          number(text, "_refine.ls_d_res_low"),
          number(text, "_refine.ls_d_res_high"),
        ),
        { mono: true },
      ),
      fact("Reflections used", count(number(text, "_refine.ls_number_reflns_obs")), { mono: true }),
      fact("R-free reflections", freeSet(text), { mono: true }),
      fact("Percent reflections observed", percent(number(text, "_refine.ls_percent_reflns_obs")), { mono: true }),
      fact("R-factor observed", rValue(number(text, "_refine.ls_r_factor_obs")), { mono: true }),
      fact("R-work", rValue(number(text, "_refine.ls_r_factor_r_work")), { mono: true }),
      fact("R-free", rValue(number(text, "_refine.ls_r_factor_r_free")), { mono: true }),
      fact("Mean isotropic B", unit(number(text, "_refine.b_iso_mean"), 1, "Å²"), { mono: true }),
    ]),

    deviations(text),
    atoms(text),
    software(text),

    // Only when the entry records more than one dataset: with one, its pH and
    // temperatures are already in the tables above.
    crystallography !== null && crystallography.datasets.length > 1
      ? {
          kind: "datasets" as const,
          key: "datasets",
          title: "Crystals and datasets",
          view: crystallography,
        }
      : null,
  ].filter((block): block is ExperimentBlock => block !== null);

  return blocks.length > 0 ? { blocks } : null;
}

/* ------------------------------------------------------------------ */
/* Sections                                                            */
/* ------------------------------------------------------------------ */

function facts(
  key: string,
  title: string,
  rows: (ExperimentFact | null)[],
): ExperimentFacts | null {
  const present = rows.filter((item): item is ExperimentFact => item !== null);
  return present.length > 0 ? { kind: "facts", key, title, facts: present } : null;
}

// An unrecorded value drops its row rather than printing a dash: these
// sections carry six or seven optional fields each, and a column of dashes is
// a column of noise. What is absent from the file is absent from the page.
function fact(
  label: string,
  value: string | null,
  options?: { mono?: boolean; wrap?: boolean },
): ExperimentFact | null {
  return value === null ? null : { label, value, ...options };
}

/**
 * The data-collection statistics, overall and in the highest-resolution shell.
 *
 * The shell is picked by its own resolution rather than by its position in the
 * file: `_reflns_shell` is a loop in some deposits and a single block in
 * others, and the loop's order is not fixed.
 */
function columns(text: string): ExperimentColumns | null {
  const rows = statistics(text);
  return rows.length === 0
    ? null
    : {
        kind: "columns",
        key: "collection",
        title: "Data collection",
        columns: ["Overall", "Highest-resolution shell"],
        rows,
      };
}

function statistics(text: string): ExperimentStatistic[] {
  const shell = highestShell(text);
  const rows: ExperimentStatistic[] = [
    {
      key: "resolution",
      label: "Resolution range",
      overall: range(
        number(text, "_reflns.d_resolution_low"),
        number(text, "_reflns.d_resolution_high"),
      ),
      shell: range(shell("d_res_low"), shell("d_res_high")),
    },
    {
      key: "reflections",
      label: "Reflections",
      overall: count(number(text, "_reflns.number_obs")),
      shell: count(shell("number_unique_all") ?? shell("number_measured_all")),
    },
    {
      key: "completeness",
      label: "Completeness",
      overall: percent(number(text, "_reflns.percent_possible_obs")),
      shell: percent(shell("percent_possible_all")),
    },
    {
      key: "redundancy",
      label: "Redundancy",
      overall: decimal(number(text, "_reflns.pdbx_redundancy"), 1),
      shell: decimal(shell("pdbx_redundancy"), 1),
    },
    {
      key: "signal",
      label: "I / σ(I)",
      overall: decimal(number(text, "_reflns.pdbx_neti_over_sigmai"), 2),
      shell: decimal(shell("meani_over_sigi_obs"), 2),
    },
    {
      key: "rmerge",
      label: "R-merge",
      overall: decimal(number(text, "_reflns.pdbx_rmerge_i_obs"), 3),
      shell: decimal(shell("rmerge_i_obs") ?? shell("pdbx_rmerge_i_obs"), 3),
    },
    {
      key: "rpim",
      label: "R-pim",
      overall: decimal(number(text, "_reflns.pdbx_rpim_i_all"), 3),
      shell: decimal(shell("pdbx_rpim_i_all"), 3),
    },
    {
      key: "cchalf",
      label: "CC½",
      overall: decimal(number(text, "_reflns.pdbx_cc_half"), 3),
      shell: decimal(shell("pdbx_cc_half"), 3),
    },
    {
      key: "wilson",
      label: "B from Wilson plot",
      overall: unit(number(text, "_reflns.b_iso_wilson_estimate"), 1, "Å²"),
      shell: null,
    },
  ];
  return rows.filter((row) => row.overall !== null || row.shell !== null);
}

/** Reader for the highest-resolution shell: the block itself when the file
 *  holds one, otherwise the loop row with the finest `d_res_high`. */
function highestShell(text: string): (tag: string) => number | null {
  const loop = cifLoop(text, "_reflns_shell");
  if (loop === null || loop.rows.length === 0) {
    return (tag) => number(text, `_reflns_shell.${tag}`);
  }
  let finest: string[] | null = null;
  let best = Number.POSITIVE_INFINITY;
  for (const row of loop.rows) {
    const high = numberOf(loopValue(loop, row, "d_res_high"));
    if (high !== null && high < best) {
      best = high;
      finest = row;
    }
  }
  const row = finest ?? loop.rows[0];
  return (tag) => numberOf(loopValue(loop, row, tag));
}

/** The programs and what each was for, as RCSB lists them. */
function software(text: string): ExperimentRows | null {
  const loop = cifLoop(text, "_software");
  const rows: { key: string; cells: (string | null)[] }[] = [];

  if (loop === null) {
    const name = cifValue(text, "_software.name");
    if (name !== null) {
      rows.push({
        key: name,
        cells: [
          name,
          sentence(cifValue(text, "_software.classification")),
          version(cifValue(text, "_software.version")),
        ],
      });
    }
  } else {
    for (const [index, row] of loop.rows.entries()) {
      const name = loopValue(loop, row, "name");
      if (name === null) {
        continue;
      }
      rows.push({
        key: `${index}-${name}`,
        cells: [
          name,
          sentence(loopValue(loop, row, "classification")),
          version(loopValue(loop, row, "version")),
        ],
      });
    }
  }

  return rows.length === 0
    ? null
    : {
        kind: "rows",
        key: "software",
        title: "Software",
        head: ["Program", "Purpose", "Version"],
        numeric: [false, false, true],
        rows,
      };
}

/**
 * How far the refined model departs from ideal geometry, restraint by
 * restraint -- RCSB's "RMS Deviations".
 *
 * The keys are the refinement program's own ("f_bond_d", "c_angle_deg"), so
 * they are printed as written: renaming them would lose the only thing that
 * ties a number to the restraint it came from.
 */
function deviations(text: string): ExperimentRows | null {
  const loop = cifLoop(text, "_refine_ls_restr");
  if (loop === null) {
    return null;
  }
  const rows: { key: string; cells: (string | null)[] }[] = [];
  for (const [index, row] of loop.rows.entries()) {
    const type = loopValue(loop, row, "type");
    const deviation = numberOf(loopValue(loop, row, "dev_ideal"));
    if (type === null || deviation === null) {
      continue;
    }
    rows.push({
      key: `${index}-${type}`,
      cells: [type, decimal(deviation, 3), count(numberOf(loopValue(loop, row, "number")))],
    });
  }
  return rows.length === 0
    ? null
    : {
        kind: "rows",
        key: "deviations",
        title: "RMS deviations",
        head: ["Restraint", "Deviation", "Count"],
        numeric: [false, true, true],
        rows,
      };
}

/** What was in the box being refined -- RCSB's "Non-Hydrogen Atoms Used in
 *  Refinement". The last cycle is the one that describes the final model. */
function atoms(text: string): ExperimentRows | null {
  const loop = cifLoop(text, "_refine_hist");
  const read = (tag: string): number | null => {
    if (loop !== null && loop.rows.length > 0) {
      return numberOf(loopValue(loop, loop.rows[loop.rows.length - 1], tag));
    }
    return number(text, `_refine_hist.${tag}`);
  };

  const counts: [string, number | null][] = [
    ["Protein", read("pdbx_number_atoms_protein")],
    ["Nucleic acid", read("pdbx_number_atoms_nucleic_acid")],
    ["Ligand", read("pdbx_number_atoms_ligand")],
    ["Solvent", read("number_atoms_solvent")],
    ["Total", read("number_atoms_total")],
    ["Residues", read("pdbx_number_residues_total")],
  ];

  const rows = counts
    .filter(([, value]) => value !== null)
    .map(([label, value]) => ({ key: label, cells: [label, count(value)] }));

  return rows.length === 0
    ? null
    : {
        kind: "rows",
        key: "atoms",
        title: "Non-hydrogen atoms used in refinement",
        head: ["Atoms", "Number"],
        numeric: [false, true],
        rows,
      };
}

/* ------------------------------------------------------------------ */
/* Values                                                              */
/* ------------------------------------------------------------------ */

function method(entry: Entry, text: string): string | null {
  const recorded = trimmed(entry.method);
  if (recorded) {
    return recorded;
  }
  const raw = cifValue(text, "_exptl.method");
  return canonicalMethod(raw ?? "") ?? sentence(raw);
}

function resolution(entry: Entry, text: string): number | null {
  return (
    finite(entry.resolution) ??
    number(text, "_refine.ls_d_res_high") ??
    number(text, "_reflns.d_resolution_high")
  );
}

function spaceGroup(entry: Entry, text: string): string | null {
  return (
    trimmed(entry.space_group) ??
    cifValue(text, "_symmetry.space_group_name_h-m") ??
    cifValue(text, "_space_group.name_h-m_alt")
  );
}

function lengths(text: string): string | null {
  const values = [
    number(text, "_cell.length_a"),
    number(text, "_cell.length_b"),
    number(text, "_cell.length_c"),
  ];
  return values.every((value) => value !== null)
    ? `${values.map((value) => fixed(value as number, 2)).join(", ")} Å`
    : null;
}

function angles(text: string): string | null {
  const values = [
    number(text, "_cell.angle_alpha"),
    number(text, "_cell.angle_beta"),
    number(text, "_cell.angle_gamma"),
  ];
  return values.every((value) => value !== null)
    ? `${values.map((value) => fixed(value as number, 2)).join(", ")}°`
    : null;
}

function wavelength(text: string): number | null {
  const loop = cifLoop(text, "_diffrn_radiation_wavelength");
  if (loop !== null && loop.rows.length > 0) {
    return numberOf(loopValue(loop, loop.rows[0], "wavelength"));
  }
  return number(text, "_diffrn_radiation_wavelength.wavelength");
}

function freeSet(text: string): string | null {
  const reflections = count(number(text, "_refine.ls_number_reflns_r_free"));
  const share = percent(number(text, "_refine.ls_percent_reflns_r_free"));
  if (reflections === null) {
    return share;
  }
  return share === null ? reflections : `${reflections} (${share})`;
}

/* ------------------------------------------------------------------ */
/* Formatting                                                          */
/* ------------------------------------------------------------------ */

function number(text: string, tag: string): number | null {
  return numberOf(cifValue(text, tag));
}

function numberOf(raw: string | null): number | null {
  if (raw === null) {
    return null;
  }
  const value = Number(raw);
  return Number.isFinite(value) ? value : null;
}

function angstrom(value: number | null, digits = 2): string | null {
  return value === null ? null : `${fixed(value, digits)} Å`;
}

function unit(value: number | null, digits: number, suffix: string): string | null {
  return value === null ? null : `${fixed(value, digits)} ${suffix}`;
}

function range(low: number | null, high: number | null): string | null {
  if (low === null || high === null) {
    return angstrom(high ?? low);
  }
  return `${fixed(low, 2)} – ${fixed(high, 2)} Å`;
}

// Three decimals, the way an R-value is quoted everywhere else.
function rValue(value: number | null): string | null {
  return value === null ? null : fixed(value, 3);
}

function decimal(value: number | null, digits: number): string | null {
  return value === null ? null : fixed(value, digits);
}

function percent(value: number | null): string | null {
  return value === null ? null : `${fixed(value, 1)}%`;
}

function count(value: number | null): string | null {
  return value === null ? null : numberFormatter.format(value);
}

function kelvin(value: number | null): string | null {
  return value === null ? null : formatKelvin(value);
}

function phValue(value: number | null): string | null {
  return value === null ? null : formatPH(value);
}

function date(raw: string | null): string | null {
  if (raw === null) {
    return null;
  }
  const parsed = new Date(`${raw}T00:00:00Z`);
  return Number.isNaN(parsed.getTime()) ? raw : dateFormatter.format(parsed);
}

// Header vocabulary comes either shouted or in lower case -- 'VAPOR DIFFUSION'
// and 'data reduction' are both from controlled lists -- and neither is what a
// label should look like. A value that is already mixed case was written by
// hand and is left as it is, bar its first letter.
function sentence(raw: string | null): string | null {
  const value = trimmed(raw ?? undefined);
  if (!value) {
    return null;
  }
  const body = value === value.toUpperCase() ? value.toLowerCase() : value;
  return body.charAt(0).toUpperCase() + body.slice(1);
}

/** "M" and "L" are the file's whole vocabulary for this. */
function monochromator(text: string): string | null {
  const value = cifValue(text, "_diffrn_radiation.pdbx_monochromatic_or_laue_m_l");
  return value === "M" ? "Monochromatic" : value === "L" ? "Laue" : null;
}

function join(
  first: string | null,
  second: string | null,
  separator = " — ",
): string | null {
  if (first === null) {
    return second;
  }
  return second === null ? first : `${first}${separator}${second}`;
}

// "(1.11.1_2575: ???)" -> "1.11.1_2575". Programs write their build after a
// colon and wrap the lot in brackets; a version with no digit in it is not a
// version and is dropped.
function version(raw: string | null): string | null {
  const value = (raw ?? "").replace(/^[('"\s]+|[)'"\s]+$/g, "").split(":")[0].trim();
  return value && /\d/.test(value) ? value : null;
}

function fixed(value: number, digits: number): string {
  return value.toFixed(digits);
}

function finite(value: number | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function trimmed(value: string | undefined): string | null {
  const text = value?.trim();
  return text ? text : null;
}

const numberFormatter = new Intl.NumberFormat("en-US");

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});
