import type {
  Artifact,
  Metric,
  ReviewEntry,
  ReviewModel,
} from "@/lib/api/entries";

export type DiffState = "added" | "removed" | "changed" | "same";

export type DiffRow = {
  label: string;
  state: DiffState;
  before: string | null;
  after: string | null;
  /** Render as word-level inline diff rather than "old -> new". */
  prose?: boolean;
  /** Signed delta between two numbers, for metrics. */
  delta?: number;
};

export type DiffSection = {
  title: string;
  rows: DiffRow[];
};

/** Only called when the entry itself was submitted — an untouched entry is not
 *  part of the review at all. */
export function diffEntry(
  active: ReviewEntry | null,
  proposed: ReviewEntry,
): DiffSection[] {
  return withRows([
    { title: "Fields", rows: entryFieldRows(active, proposed) },
    { title: "Files", rows: fileRows(active?.artifacts, proposed.artifacts) },
  ]);
}

export function diffModel(
  active: ReviewModel | null,
  proposed: ReviewModel,
): DiffSection[] {
  return withRows([
    { title: "Fields", rows: modelFieldRows(active, proposed) },
    { title: "Metrics", rows: metricRows(active?.metrics, proposed.metrics) },
    { title: "Files", rows: fileRows(active?.artifacts, proposed.artifacts) },
  ]);
}

export function countChanges(sections: DiffSection[]): number {
  return sections.reduce(
    (total, section) =>
      total + section.rows.filter((row) => row.state !== "same").length,
    0,
  );
}

function withRows(sections: DiffSection[]): DiffSection[] {
  return sections.filter((section) => section.rows.length > 0);
}

function entryFieldRows(
  before: ReviewEntry | null,
  after: ReviewEntry,
): DiffRow[] {
  return [
    scalarRow("Title", before?.title, after.title),
    ...metadataRows(before?.metadata, after.metadata),
    scalarRow(
      "Protein sequences",
      before ? String(before.protein_sequences.length) : undefined,
      String(after.protein_sequences.length),
    ),
  ];
}

function modelFieldRows(
  before: ReviewModel | null,
  after: ReviewModel,
): DiffRow[] {
  return [
    scalarRow("Title", before?.title, after.title),
    ...metadataRows(before?.metadata, after.metadata),
  ];
}

function metadataRows(
  before: Record<string, unknown> | undefined,
  after: Record<string, unknown> | undefined,
): DiffRow[] {
  const keys = new Set<string>();
  for (const key of Object.keys(before ?? {})) keys.add(key);
  for (const key of Object.keys(after ?? {})) keys.add(key);

  const rows: DiffRow[] = [];
  for (const key of [...keys].sort()) {
    rows.push(
      scalarRow(
        key,
        before ? formatValue(before[key]) : undefined,
        formatValue((after ?? {})[key]),
      ),
    );
  }
  return rows;
}

function metricRows(
  before: Metric[] | undefined,
  after: Metric[] | undefined,
): DiffRow[] {
  const beforeByKey = byKey(before);
  const afterByKey = byKey(after);
  const keys = new Set([...beforeByKey.keys(), ...afterByKey.keys()]);

  const rows: DiffRow[] = [];
  for (const key of [...keys].sort()) {
    const beforeValue = beforeByKey.get(key);
    const afterValue = afterByKey.get(key);
    const row = scalarRow(
      key,
      before ? formatNumber(beforeValue) : undefined,
      formatNumber(afterValue),
    );
    if (
      row.state === "changed" &&
      beforeValue !== undefined &&
      afterValue !== undefined
    ) {
      row.delta = afterValue - beforeValue;
    }
    rows.push(row);
  }
  return rows;
}

/** Artifact rows are duplicated per revision — every submission mints fresh
 *  artifact ids for the same file — so files are keyed on name, and the content
 *  hash is what decides whether the file actually changed. */
function fileRows(
  before: Artifact[] | undefined,
  after: Artifact[] | undefined,
): DiffRow[] {
  const beforeByName = byName(before);
  const afterByName = byName(after);
  const names = new Set([...beforeByName.keys(), ...afterByName.keys()]);

  const rows: DiffRow[] = [];
  for (const name of [...names].sort()) {
    const beforeFile = beforeByName.get(name);
    const afterFile = afterByName.get(name);

    if (!beforeFile && afterFile) {
      rows.push(row(name, "added", null, describeFile(afterFile)));
      continue;
    }
    if (beforeFile && !afterFile) {
      rows.push(row(name, "removed", describeFile(beforeFile), null));
      continue;
    }
    if (!beforeFile || !afterFile) continue;

    const changed =
      beforeFile.sha256 !== null && afterFile.sha256 !== null
        ? beforeFile.sha256 !== afterFile.sha256
        : beforeFile.size_bytes !== afterFile.size_bytes;
    rows.push(
      row(
        name,
        changed ? "changed" : "same",
        describeFile(beforeFile),
        describeFile(afterFile),
      ),
    );
  }
  return rows;
}

function scalarRow(
  label: string,
  before: string | null | undefined,
  after: string | null | undefined,
  prose = false,
): DiffRow {
  const beforeValue = normalize(before);
  const afterValue = normalize(after);

  if (before === undefined) return { ...row(label, "added", null, afterValue), prose };
  if (beforeValue === null && afterValue !== null)
    return { ...row(label, "added", null, afterValue), prose };
  if (beforeValue !== null && afterValue === null)
    return { ...row(label, "removed", beforeValue, null), prose };
  if (beforeValue === afterValue)
    return { ...row(label, "same", beforeValue, afterValue), prose };
  return { ...row(label, "changed", beforeValue, afterValue), prose };
}

function row(
  label: string,
  state: DiffState,
  before: string | null,
  after: string | null,
): DiffRow {
  return { label, state, before, after };
}

function normalize(value: string | null | undefined): string | null {
  if (value === undefined || value === null) return null;
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

function byKey(metrics: Metric[] | undefined): Map<string, number> {
  const result = new Map<string, number>();
  for (const metric of metrics ?? []) result.set(metric.key, metric.value);
  return result;
}

function byName(artifacts: Artifact[] | undefined): Map<string, Artifact> {
  const result = new Map<string, Artifact>();
  for (const artifact of artifacts ?? []) result.set(artifact.name, artifact);
  return result;
}

function describeFile(artifact: Artifact): string {
  const parts: string[] = [];
  if (artifact.format) parts.push(artifact.format);
  if (artifact.size_bytes !== null) parts.push(formatBytes(artifact.size_bytes));
  return parts.join(" · ") || "file";
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
}

function formatNumber(value: number | undefined): string | null {
  return value === undefined ? null : String(value);
}

function formatValue(value: unknown): string | null {
  if (value === null || value === undefined) return null;
  if (Array.isArray(value)) return value.map((item) => formatValue(item)).join(", ");
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

/** Splits two strings into a shared head, the differing middles and a shared
 *  tail, so prose changes read as an edit instead of a full replacement. */
export function inlineDiff(
  before: string,
  after: string,
): { head: string; removed: string; added: string; tail: string } {
  const beforeParts = before.split(/(\s+)/);
  const afterParts = after.split(/(\s+)/);

  let head = 0;
  while (
    head < beforeParts.length &&
    head < afterParts.length &&
    beforeParts[head] === afterParts[head]
  ) {
    head += 1;
  }

  let tail = 0;
  while (
    tail < beforeParts.length - head &&
    tail < afterParts.length - head &&
    beforeParts[beforeParts.length - 1 - tail] ===
      afterParts[afterParts.length - 1 - tail]
  ) {
    tail += 1;
  }

  return {
    head: beforeParts.slice(0, head).join(""),
    removed: beforeParts.slice(head, beforeParts.length - tail).join(""),
    added: afterParts.slice(head, afterParts.length - tail).join(""),
    tail: beforeParts.slice(beforeParts.length - tail).join(""),
  };
}
