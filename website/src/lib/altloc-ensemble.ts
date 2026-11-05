// A viewer-only copy of an ensemble deposited as altlocs: every whole-chain altloc letter becomes
// one MODEL. The heterogeneity viewer's frame stepper and ensemble RMSF read MODEL numbers, and a
// file such as 44PS holds all six conformers in one MODEL under letters A to F.
//
// Nothing here is stored: the record and the download links keep the deposited file. Only models
// typed Ensemble go through this, because a qFit model's letters are local to each residue and do
// not trace a conformer along the chain.

type AtomSiteColumns = {
  id: number;
  group: number;
  element: number;
  altId: number;
  chain: number;
  seq: number;
  insCode: number;
  model: number;
};

type ParsedAtomSite = {
  headerEnd: number;
  bodyEnd: number;
  loopStart: number;
  columns: AtomSiteColumns;
  rows: string[][];
};

export type AltlocEnsembleFile =
  | { kind: "converted"; text: string; letters: string[] }
  /** The file already lists several MODELs, which the viewer reads as they are. */
  | { kind: "multi-model" }
  /** No letter set covers every polymer residue, so the letters are not whole-chain states. */
  | { kind: "not-an-altloc-ensemble" };

const UNSET_VALUES = new Set([".", "?"]);

/** The CIF values of one line, each with its quotes, since rows are written back as read. */
function lineTokens(line: string): string[] {
  const tokens: string[] = [];
  let index = 0;
  while (index < line.length) {
    const character = line[index];
    if (character === " " || character === "\t" || character === "\r") {
      index += 1;
      continue;
    }
    if (character === "#") {
      break;
    }
    if (character === "'" || character === '"') {
      // A quote ends a value only before whitespace: an atom named C1' sits inside "C1'".
      let end = index + 1;
      while (end < line.length) {
        const after = line[end + 1];
        if (line[end] === character && (after === undefined || /\s/.test(after))) {
          break;
        }
        end += 1;
      }
      tokens.push(line.slice(index, end + 1));
      index = end + 1;
      continue;
    }
    let end = index;
    while (end < line.length && !/\s/.test(line[end])) {
      end += 1;
    }
    tokens.push(line.slice(index, end));
    index = end;
  }
  return tokens;
}

function unquoted(value: string): string {
  const first = value[0];
  if ((first === "'" || first === '"') && value.at(-1) === first && value.length >= 2) {
    return value.slice(1, -1);
  }
  return value;
}

function loopEnd(lines: string[], start: number): number {
  let index = start;
  while (index < lines.length) {
    const line = lines[index];
    if (line.startsWith("#") || line.startsWith("loop_") || line.startsWith("_") || line.startsWith("data_")) {
      break;
    }
    index += 1;
  }
  return index;
}

/** Where the loop whose items start with prefix begins, or -1. */
function loopWithItems(lines: string[], prefix: string): number {
  for (let index = 0; index + 1 < lines.length; index += 1) {
    if (lines[index].trim() === "loop_" && lines[index + 1].toLowerCase().startsWith(prefix)) {
      return index;
    }
  }
  return -1;
}

function parseAtomSite(lines: string[]): ParsedAtomSite | null {
  const loopStart = loopWithItems(lines, "_atom_site.");
  if (loopStart === -1) {
    return null;
  }
  let headerEnd = loopStart + 1;
  const names = new Map<string, number>();
  while (headerEnd < lines.length && lines[headerEnd].startsWith("_")) {
    names.set(lines[headerEnd].trim().slice("_atom_site.".length).toLowerCase(), names.size);
    headerEnd += 1;
  }
  const column = (name: string) => names.get(name) ?? -1;
  const columns: AtomSiteColumns = {
    id: column("id"),
    group: column("group_pdb"),
    element: column("type_symbol"),
    altId: column("label_alt_id"),
    chain: column("auth_asym_id") === -1 ? column("label_asym_id") : column("auth_asym_id"),
    seq: column("auth_seq_id") === -1 ? column("label_seq_id") : column("auth_seq_id"),
    insCode: column("pdbx_pdb_ins_code"),
    model: column("pdbx_pdb_model_num"),
  };
  if (columns.altId === -1 || columns.model === -1) {
    return null;
  }
  const bodyEnd = loopEnd(lines, headerEnd);
  // A row may wrap over lines, so values are gathered until a row is full.
  const rows: string[][] = [];
  let row: string[] = [];
  for (let index = headerEnd; index < bodyEnd; index += 1) {
    for (const token of lineTokens(lines[index])) {
      row.push(token);
      if (row.length === names.size) {
        rows.push(row);
        row = [];
      }
    }
  }
  return { headerEnd, bodyEnd, loopStart, columns, rows };
}

function isUnset(value: string): boolean {
  return UNSET_VALUES.has(value);
}

/** The letters on every polymer residue, as lib/cif counts a model's members. */
function wholeChainLetters(site: ParsedAtomSite): string[] {
  const { columns, rows } = site;
  const lettersByResidue = new Map<string, Set<string>>();
  for (const row of rows) {
    if (columns.group !== -1 && row[columns.group].toUpperCase() !== "ATOM") {
      continue;
    }
    const element = columns.element === -1 ? "" : row[columns.element].toUpperCase();
    if (element === "H" || element === "D") {
      continue;
    }
    const residue = [
      row[columns.chain] ?? "",
      row[columns.seq] ?? "",
      columns.insCode === -1 ? "" : row[columns.insCode],
    ].join("|");
    const letters = lettersByResidue.get(residue) ?? new Set<string>();
    const letter = unquoted(row[columns.altId]);
    if (!isUnset(letter)) {
      letters.add(letter);
    }
    lettersByResidue.set(residue, letters);
  }
  const [first, ...others] = [...lettersByResidue.values()];
  const shared = (first ? [...first] : []).filter((letter) =>
    others.every((letters) => letters.has(letter)),
  );
  return shared.sort();
}

function withoutLoop(lines: string[], prefix: string): string[] {
  const start = loopWithItems(lines, prefix);
  if (start === -1) {
    return lines;
  }
  let itemsEnd = start + 1;
  while (itemsEnd < lines.length && lines[itemsEnd].startsWith("_")) {
    itemsEnd += 1;
  }
  return [...lines.slice(0, start), ...lines.slice(loopEnd(lines, itemsEnd))];
}

/** Rewrites an mmCIF file so each whole-chain altloc letter is one MODEL. */
export function altlocEnsembleFile(text: string): AltlocEnsembleFile {
  // The anisotropic rows point at atom ids this rewrite renumbers.
  const lines = withoutLoop(text.split("\n"), "_atom_site_anisotrop.");
  const site = parseAtomSite(lines);
  if (!site) {
    return { kind: "not-an-altloc-ensemble" };
  }
  const { columns, rows } = site;
  if (new Set(rows.map((row) => row[columns.model])).size > 1) {
    return { kind: "multi-model" };
  }
  const letters = wholeChainLetters(site);
  if (letters.length < 2) {
    return { kind: "not-an-altloc-ensemble" };
  }
  const members = new Set(letters);
  const frameRows: string[] = [];
  let nextId = 1;
  letters.forEach((letter, memberIndex) => {
    for (const row of rows) {
      const rowLetter = unquoted(row[columns.altId]);
      const isMember = members.has(rowLetter);
      if (isMember && rowLetter !== letter) {
        continue;
      }
      const frameRow = [...row];
      if (isMember) {
        frameRow[columns.altId] = ".";
      }
      frameRow[columns.model] = String(memberIndex + 1);
      if (columns.id !== -1) {
        frameRow[columns.id] = String(nextId);
      }
      nextId += 1;
      frameRows.push(frameRow.join(" "));
    }
  });
  return {
    kind: "converted",
    letters,
    text: [...lines.slice(0, site.headerEnd), ...frameRows, ...lines.slice(site.bodyEnd)].join("\n"),
  };
}
