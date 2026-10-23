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

// Loops wider than this are skipped, as in lib/cif.
const MAX_LOOP_COLUMNS = 1000;

/**
 * Reads whitespace-separated tokens across lines, the way lib/cif does: bare
 * and quoted values, `;` text blocks that start in column one, and `#`
 * comments. Each character is visited once, so a read costs the length of
 * the text however the rows are wrapped.
 */
export class CifTokens {
  value = "";
  quoted = false;
  position = 0;

  constructor(private readonly text: string) {}

  next(): boolean {
    const text = this.text;
    while (this.position < text.length) {
      const start = this.position;
      const char = text[start];
      if (char === " " || char === "\t" || char === "\n" || char === "\r") {
        this.position += 1;
      } else if (char === "#") {
        const end = text.indexOf("\n", start);
        this.position = end === -1 ? text.length : end;
      } else if (char === ";" && (start === 0 || text[start - 1] === "\n")) {
        const end = text.indexOf("\n;", start + 1);
        this.value = text.slice(start + 1, end === -1 ? text.length : end);
        this.quoted = true;
        this.position = end === -1 ? text.length : end + 2;
        return true;
      } else if (char === "'" || char === '"') {
        // The closing quote is the first matching one followed by whitespace;
        // an unterminated value takes the rest of the line.
        let cursor = start + 1;
        let closed = false;
        while (cursor < text.length && text[cursor] !== "\n") {
          if (text[cursor] === char) {
            const after = text[cursor + 1];
            if (after === undefined || after === " " || after === "\t" || after === "\n" || after === "\r") {
              closed = true;
              break;
            }
          }
          cursor += 1;
        }
        this.value = text.slice(start + 1, cursor);
        this.quoted = true;
        this.position = closed ? cursor + 1 : cursor;
        return true;
      } else {
        let end = start;
        while (end < text.length && !" \t\n\r".includes(text[end])) {
          end += 1;
        }
        this.value = text.slice(start, end);
        this.quoted = false;
        this.position = end;
        return true;
      }
    }
    return false;
  }

  /** Steps back over the unquoted token `next` just returned. */
  unreadBare(): void {
    this.position -= this.value.length;
  }
}

function endsLoop(value: string): boolean {
  const lower = value.toLowerCase();
  return (
    value.startsWith("_") ||
    lower.startsWith("data_") ||
    lower === "loop_" ||
    lower.startsWith("save_") ||
    lower === "global_" ||
    lower === "stop_"
  );
}

/**
 * The first loop whose tags belong to `category` (given without the trailing
 * dot). Values are read as one stream across lines and cut into rows by the
 * column count, as lib/cif does, so a row may wrap over several lines and a
 * value may be quoted or a `;` text block. Trailing values that do not fill a
 * row are dropped.
 */
export function cifLoop(text: string, category: string): CifLoop | null {
  const prefix = `${category.toLowerCase()}.`;
  const tokens = new CifTokens(text);

  while (tokens.next()) {
    if (tokens.quoted || tokens.value.toLowerCase() !== "loop_") {
      continue;
    }
    const columns: string[] = [];
    let more = false;
    while (tokens.next()) {
      if (tokens.quoted || !tokens.value.startsWith("_")) {
        more = true;
        break;
      }
      columns.push(tokens.value.toLowerCase());
    }
    if (columns.length === 0 || !columns[0].startsWith(prefix)) {
      if (more && !tokens.quoted && endsLoop(tokens.value)) {
        tokens.unreadBare();
      }
      continue;
    }

    const width = columns.length;
    const rows: string[][] = [];
    if (more && width <= MAX_LOOP_COLUMNS) {
      let row: string[] = [];
      do {
        if (!tokens.quoted && endsLoop(tokens.value)) {
          break;
        }
        row.push(tokens.value);
        if (row.length === width) {
          rows.push(row);
          row = [];
        }
      } while (tokens.next());
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
  const value = row[index]?.trim();
  return value === undefined || isMissing(value) ? null : value;
}
