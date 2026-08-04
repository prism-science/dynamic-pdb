// A deliberately small mmCIF reader: enough to pull single values out of a
// category and to walk one loop. Not a validating parser — anything it cannot
// make sense of comes back empty, and the caller treats that the same as a file
// with no header at all.
//
// molstar ships a complete CIF reader, but pulling it in costs the whole
// mol-io tree in a form bundle for the sake of a dozen tags.

export type CifLoop = {
  columns: string[];
  rows: string[][];
};

// Splits a data line into bare, single-quoted and double-quoted tokens.
function tokenize(line: string): string[] {
  const tokens: string[] = [];
  let index = 0;
  while (index < line.length) {
    const char = line[index];
    if (char === " " || char === "\t") {
      index += 1;
      continue;
    }
    if (char === "'" || char === '"') {
      const end = line.indexOf(char, index + 1);
      if (end === -1) {
        tokens.push(line.slice(index + 1));
        break;
      }
      tokens.push(line.slice(index + 1, end));
      index = end + 1;
      continue;
    }
    let end = index;
    while (end < line.length && line[end] !== " " && line[end] !== "\t") {
      end += 1;
    }
    tokens.push(line.slice(index, end));
    index = end;
  }
  return tokens;
}

function isMissing(value: string): boolean {
  return value === "?" || value === "." || value === "";
}

/**
 * Value of a single `_category.tag` outside any loop. Multi-line semicolon
 * blocks are joined into one string.
 */
export function cifValue(text: string, tag: string): string | null {
  const lines = text.split("\n");
  const needle = tag.toLowerCase();
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (!line.startsWith("_")) {
      continue;
    }
    const tokens = tokenize(line);
    if (tokens[0]?.toLowerCase() !== needle) {
      continue;
    }
    if (tokens.length > 1) {
      const value = tokens.slice(1).join(" ").trim();
      return isMissing(value) ? null : value;
    }
    // Value on the following line, possibly a `;`-delimited block.
    const next = lines[index + 1] ?? "";
    if (next.startsWith(";")) {
      const parts = [next.slice(1)];
      let cursor = index + 2;
      while (cursor < lines.length && !lines[cursor].startsWith(";")) {
        parts.push(lines[cursor]);
        cursor += 1;
      }
      const value = parts.join(" ").trim();
      return isMissing(value) ? null : value;
    }
    const value = tokenize(next).join(" ").trim();
    return isMissing(value) ? null : value;
  }
  return null;
}

/**
 * The first loop whose tags belong to `category` (given without the trailing
 * dot). Rows shorter than the column list are dropped rather than padded: a
 * truncated row means the line wrapped in a way this reader does not follow,
 * and guessing would be worse than skipping.
 */
export function cifLoop(text: string, category: string): CifLoop | null {
  const lines = text.split("\n");
  const prefix = `${category.toLowerCase()}.`;

  for (let index = 0; index < lines.length; index += 1) {
    if (lines[index].trim().toLowerCase() !== "loop_") {
      continue;
    }
    const columns: string[] = [];
    let cursor = index + 1;
    while (cursor < lines.length && lines[cursor].trimStart().startsWith("_")) {
      columns.push(lines[cursor].trim().toLowerCase());
      cursor += 1;
    }
    if (columns.length === 0 || !columns[0].startsWith(prefix)) {
      continue;
    }

    const rows: string[][] = [];
    while (cursor < lines.length) {
      const line = lines[cursor];
      const trimmed = line.trim();
      if (trimmed === "" || trimmed === "#" || trimmed.startsWith("_") || trimmed.toLowerCase() === "loop_" || trimmed.startsWith("data_")) {
        break;
      }
      const tokens = tokenize(line);
      if (tokens.length >= columns.length) {
        rows.push(tokens.slice(0, columns.length));
      }
      cursor += 1;
    }

    return {
      columns: columns.map((column) => column.slice(prefix.length)),
      rows,
    };
  }
  return null;
}

export function loopColumn(loop: CifLoop, name: string): number {
  return loop.columns.indexOf(name.toLowerCase());
}

export function loopValue(
  loop: CifLoop,
  row: string[],
  name: string,
): string | null {
  const index = loopColumn(loop, name);
  if (index === -1) {
    return null;
  }
  const value = row[index];
  return value === undefined || isMissing(value) ? null : value;
}
