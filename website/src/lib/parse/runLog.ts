// What a run log says it consumed and produced.
//
// A refinement log is the only place where the inputs are written down: a PDB
// header records which program made the coordinates but never which reflection
// file it was refined against. phenix, Refmac and qFit each state it plainly,
// just in three different dialects.
//
// Every parser here is deliberately conservative. A log it cannot read comes
// back as null, and null is treated as "no log" — never as licence to guess.

export type RunLogFacts = {
  program: { name: string; version: string | null };
  // Basenames as written in the log. Matching them to uploaded files is the
  // caller's job: the paths point at the author's own disk.
  inputs: string[];
  outputs: string[];
};

const COORDINATE = /\.(pdb|cif|mmcif|ent)$/i;
const REFLECTIONS = /\.(mtz|sca|hkl|cv)$/i;
const MAP = /\.(ccp4|map|mrc)$/i;

export function looksLikeRunLog(name: string): boolean {
  return /\.(log|eff|def|out|txt)$/i.test(name);
}

export function parseRunLog(text: string): RunLogFacts | null {
  return parsePhenix(text) ?? parseRefmac(text) ?? parseQFit(text);
}

/* ------------------------------------------------------------- phenix -- */

function parsePhenix(text: string): RunLogFacts | null {
  const banner =
    /phenix\.(refine|real_space_refine)\b/i.exec(text) ??
    (/\brefinement\s*\{/.test(text) && /\bpdb\s*\{/.test(text)
      ? ["phenix.refine", "refine"]
      : null);
  if (!banner) {
    return null;
  }
  const name = banner[0].toLowerCase().startsWith("phenix.")
    ? banner[0].toLowerCase()
    : "phenix.refine";

  // Real logs print the version either on its own line or straight after the
  // subcommand in the banner.
  const version =
    /^\s*Version:?\s*([0-9][0-9A-Za-z._-]*)/im.exec(text)?.[1] ??
    /phenix\.[a-z_]+\s+v?([0-9][0-9A-Za-z._-]*)/i.exec(text)?.[1] ??
    /\bphenix[- ]v?([0-9][0-9A-Za-z._-]*)/i.exec(text)?.[1] ??
    null;

  const inputs = new Set<string>();
  const outputs = new Set<string>();

  // Parameter blocks (.eff / .def and the echo at the top of a .log) name the
  // inputs outright.
  for (const match of text.matchAll(/file_name\s*=\s*"?([^"\s]+)"?/gi)) {
    const file = basename(match[1]);
    if (COORDINATE.test(file) || REFLECTIONS.test(file) || /\.cif$/i.test(file)) {
      inputs.add(file);
    }
  }
  for (const match of text.matchAll(
    /^\s*(?:Input|Reading)\s+(?:PDB|model|reflection|data)\s*file\s*:?\s*(\S+)/gim,
  )) {
    inputs.add(basename(match[1]));
  }

  // Outputs are announced as a written-file list.
  for (const match of text.matchAll(
    /^\s*(?:Writing|Wrote|Output)\s+(?:model|map coefficients|file)?\s*:?\s*(\S+\.(?:pdb|cif|mtz|ccp4|map))\b/gim,
  )) {
    outputs.add(basename(match[1]));
  }
  const prefix = /output\s*\{[^}]*?prefix\s*=\s*"?([^"\s}]+)/is.exec(text)?.[1];
  const serial = /output\s*\{[^}]*?serial\s*=\s*([0-9]+)/is.exec(text)?.[1];
  if (prefix && outputs.size === 0) {
    const stem = serial ? `${prefix}_${serial.padStart(3, "0")}` : prefix;
    outputs.add(`${basename(stem)}.pdb`);
    outputs.add(`${basename(stem)}_map_coeffs.mtz`);
  }

  return finish(name, version, inputs, outputs);
}

/* ------------------------------------------------------------- refmac -- */

function parseRefmac(text: string): RunLogFacts | null {
  if (!/refmac/i.test(text)) {
    return null;
  }
  const version =
    /Refmac_?\s*([0-9][0-9A-Za-z._-]*)/i.exec(text)?.[1] ??
    /version\s+([0-9][0-9A-Za-z._-]*)/i.exec(text)?.[1] ??
    null;

  const inputs = new Set<string>();
  const outputs = new Set<string>();
  // CCP4 programs take their files as logical-name assignments.
  for (const match of text.matchAll(/^\s*(XYZIN|HKLIN|LIBIN|TLSIN)\s+(\S+)/gim)) {
    inputs.add(basename(match[2]));
  }
  for (const match of text.matchAll(/^\s*(XYZOUT|HKLOUT|LIBOUT|TLSOUT)\s+(\S+)/gim)) {
    outputs.add(basename(match[2]));
  }

  return finish("refmac5", version, inputs, outputs);
}

/* --------------------------------------------------------------- qfit -- */

function parseQFit(text: string): RunLogFacts | null {
  if (!/\bqfit\b/i.test(text)) {
    return null;
  }
  const version = /qfit[- ]?(?:protein)?\s*v?([0-9][0-9A-Za-z._-]*)/i.exec(text)?.[1] ?? null;

  const inputs = new Set<string>();
  const outputs = new Set<string>();
  // qFit records its command line: `qfit_protein map.mtz model.pdb -d out`.
  const command = /^.*\bqfit[_a-z]*\s+(.+)$/im.exec(text)?.[1] ?? "";
  for (const token of command.split(/\s+/)) {
    const file = basename(token);
    if (MAP.test(file) || REFLECTIONS.test(file) || COORDINATE.test(file)) {
      inputs.add(file);
    }
  }
  for (const match of text.matchAll(/\b(multiconformer\S*\.(?:pdb|cif))\b/gi)) {
    outputs.add(basename(match[1]));
  }

  return finish("qFit", version, inputs, outputs);
}

/* ------------------------------------------------------------- shared -- */

function finish(
  name: string,
  version: string | null,
  inputs: Set<string>,
  outputs: Set<string>,
): RunLogFacts | null {
  // A program name alone is not provenance. Without at least one file on
  // either side there is nothing to attach to the graph, so the log is no more
  // useful than its absence.
  for (const output of outputs) {
    inputs.delete(output);
  }
  if (inputs.size === 0 && outputs.size === 0) {
    return null;
  }
  return {
    program: { name, version },
    inputs: [...inputs],
    outputs: [...outputs],
  };
}

export function basename(path: string): string {
  const cleaned = path.trim().replace(/["',]+$/, "");
  const parts = cleaned.split(/[\\/]/);
  return parts[parts.length - 1] ?? cleaned;
}
