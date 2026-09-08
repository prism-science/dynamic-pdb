/**
 * Just enough mmCIF to read the loops we draw tracks from.
 *
 * Not a general parser: it reads `loop_` tables into rows of strings and
 * ignores everything else. That is all the feature viewer needs, and a full
 * dictionary-aware reader would be several thousand lines for no extra rows.
 *
 * The format, in the part we care about: a `loop_` line, then one
 * `_category.item` line per column, then whitespace-separated data rows until
 * something that is not a data row. Values may be quoted with ' or ", and a
 * value on its own may be a multi-line block delimited by a `;` in column one.
 */
export type CifLoop = {
  category: string;
  columns: string[];
  rows: string[][];
};

export function parseCifLoops(text: string): Map<string, CifLoop> {
  const loops = new Map<string, CifLoop>();
  const lines = text.split(/\r?\n/);
  let index = 0;

  while (index < lines.length) {
    if (lines[index].trim() !== "loop_") {
      index += 1;
      continue;
    }
    index += 1;

    const columns: string[] = [];
    let category = "";
    while (index < lines.length && lines[index].trim().startsWith("_")) {
      const tag = lines[index].trim().split(/\s+/)[0];
      const dot = tag.indexOf(".");
      category = dot === -1 ? tag : tag.slice(0, dot);
      columns.push(dot === -1 ? tag : tag.slice(dot + 1));
      index += 1;
    }
    if (columns.length === 0) {
      continue;
    }

    const rows: string[][] = [];
    let pending: string[] = [];
    while (index < lines.length) {
      const line = lines[index];
      const trimmed = line.trim();
      if (trimmed === "" || trimmed === "#" || trimmed === "loop_") {
        break;
      }
      if (trimmed.startsWith("_") || trimmed.startsWith("data_")) {
        break;
      }
      // A semicolon in column one opens a multi-line value; it counts as one
      // token however many lines it spans.
      if (line.startsWith(";")) {
        const block: string[] = [line.slice(1)];
        index += 1;
        while (index < lines.length && !lines[index].startsWith(";")) {
          block.push(lines[index]);
          index += 1;
        }
        index += 1;
        pending.push(block.join("\n").trim());
      } else {
        pending.push(...tokenize(line));
        index += 1;
      }
      while (pending.length >= columns.length) {
        rows.push(pending.slice(0, columns.length));
        pending = pending.slice(columns.length);
      }
    }

    // Later loops of the same category replace earlier ones rather than
    // merging: a file with two `_atom_site` loops is malformed, and guessing
    // how to join them would invent data.
    loops.set(category, { category, columns, rows });
  }

  return loops;
}

export function columnIndex(loop: CifLoop, name: string): number {
  return loop.columns.indexOf(name);
}

/** "." and "?" are mmCIF's "not applicable" and "unknown"; both mean absent. */
export function cifValue(value: string | undefined): string | null {
  if (value === undefined || value === "." || value === "?") {
    return null;
  }
  return value;
}

export function cifNumber(value: string | undefined): number | null {
  const raw = cifValue(value);
  if (raw === null) {
    return null;
  }
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
}

function tokenize(line: string): string[] {
  const tokens: string[] = [];
  let position = 0;
  while (position < line.length) {
    const char = line[position];
    if (char === " " || char === "\t") {
      position += 1;
      continue;
    }
    if (char === "#") {
      break;
    }
    if (char === "'" || char === '"') {
      const end = line.indexOf(char, position + 1);
      if (end === -1) {
        tokens.push(line.slice(position + 1));
        break;
      }
      tokens.push(line.slice(position + 1, end));
      position = end + 1;
      continue;
    }
    let end = position;
    while (end < line.length && line[end] !== " " && line[end] !== "\t") {
      end += 1;
    }
    tokens.push(line.slice(position, end));
    position = end;
  }
  return tokens;
}
