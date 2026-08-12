import type { SimilarEntry, SimilarEntryMatch } from "@/lib/api/entries";

/**
 * What the similarity runner recorded about one pairwise hit. The metadata is
 * a free-form JSON object, so which keys are present depends on the tool and
 * on when the run was made. Every field is optional and the view degrades:
 * without positions the matched-region tracks are approximated from
 * coverage, without valid aligned strings there is no residue comparison.
 */
export type SimilarityStats = {
  fident: number | null;
  qcov: number | null;
  tcov: number | null;
  evalue: number | null;
  bits: number | null;
  qstart: number | null;
  qend: number | null;
  tstart: number | null;
  tend: number | null;
  qaln: string | null;
  taln: string | null;
};

export function similarityStats(
  metadata: Record<string, unknown> | undefined,
): SimilarityStats {
  const source = metadata ?? {};
  // Canonical keys follow the seed/runner convention (source_*/similar_*,
  // matching source_sequence_id/similar_sequence_id); the mmseqs-native
  // spellings are accepted as a fallback.
  return {
    fident: numberField(source.fident),
    qcov: numberField(source.qcov),
    tcov: numberField(source.tcov),
    evalue: numberField(source.evalue),
    bits: numberField(source.bits),
    qstart: numberField(source.source_start ?? source.qstart),
    qend: numberField(source.source_end ?? source.qend),
    tstart: numberField(source.similar_start ?? source.tstart),
    tend: numberField(source.similar_end ?? source.tend),
    qaln: stringField(source.source_alignment ?? source.qaln),
    taln: stringField(source.similar_alignment ?? source.taln),
  };
}

/**
 * Whether the recorded aligned strings are actually usable: same length,
 * residues-and-gaps only, and — where the raw sequences are at hand — the
 * degapped strings must reproduce the sequences at the recorded spans.
 * Anything else (placeholders, truncated writes, span drift) is treated as
 * "not recorded", and the UI says so instead of drawing a wrong alignment.
 */
export function hasRecordedAlignment(
  stats: SimilarityStats,
  sourceSequence: string | null,
  similarSequence: string,
): boolean {
  const { qaln, taln, qstart, qend, tstart, tend } = stats;
  if (!qaln || !taln || qaln.length !== taln.length) {
    return false;
  }
  if (!/^[A-Z-]+$/.test(qaln) || !/^[A-Z-]+$/.test(taln)) {
    return false;
  }
  if (sourceSequence && qstart != null && qend != null) {
    const expected = sourceSequence.slice(qstart - 1, qend).toUpperCase();
    if (qaln.replace(/-/g, "") !== expected) {
      return false;
    }
  }
  if (tstart != null && tend != null) {
    const expected = similarSequence.slice(tstart - 1, tend).toUpperCase();
    if (taln.replace(/-/g, "") !== expected) {
      return false;
    }
  }
  return true;
}

/**
 * The strongest numbers across a hit's per-chain matches, for one-line
 * summaries in the rail and the table: best identity and best coverage.
 * Per-chain detail lives in the expanded alignment view.
 */
export function bestMatchStats(matches: SimilarEntryMatch[]): {
  fident: number | null;
  qcov: number | null;
} {
  let fident: number | null = null;
  let qcov: number | null = null;
  for (const match of matches) {
    const stats = similarityStats(match.metadata);
    if (stats.fident != null && (fident == null || stats.fident > fident)) {
      fident = stats.fident;
    }
    if (stats.qcov != null && (qcov == null || stats.qcov > qcov)) {
      qcov = stats.qcov;
    }
  }
  return { fident, qcov };
}

/**
 * "2MHB_1|Chain A|Hemoglobin subunit alpha|Equus caballus" -> "2MHB:A".
 *
 * Deflines inside one file are written by one producer (see SequenceView), so
 * the first field is an accession — possibly with a record suffix — and a
 * "Chain X" field may follow. Headers from elsewhere may have neither: fall
 * back to the first token, and finally to the record number, so the label
 * stays short enough to caption an alignment row.
 */
export function sequenceLabel(header: string, recordIndex: number): string {
  const fields = header
    .replace(/^>/, "")
    .trim()
    .split("|")
    .map((field) => field.trim())
    .filter(Boolean);
  if (fields.length === 0) {
    return `seq ${recordIndex + 1}`;
  }
  const accession = (fields[0].split(/\s+/)[0] ?? "").replace(/_\d+$/, "");
  if (!accession) {
    return `seq ${recordIndex + 1}`;
  }
  const base =
    accession.length > 12 ? `${accession.slice(0, 11)}…` : accession;
  const chainField = fields.find((field) => /^Chains?\s+/i.test(field));
  const chains = chainField?.replace(/^Chains?\s+/i, "");
  return chains ? `${base}:${chains}` : base;
}

/**
 * Everything after the accession and chain fields — the human-readable part
 * of the defline (protein name, organism), shown next to the chain switcher.
 */
export function deflineTail(header: string): string[] {
  const fields = header
    .replace(/^>/, "")
    .trim()
    .split("|")
    .map((field) => field.trim())
    .filter(Boolean);
  const rest = fields.slice(1);
  if (rest.length > 0 && /^Chains?\s+/i.test(rest[0])) {
    return rest.slice(1);
  }
  return rest;
}

/** 0.865 -> "86.5%", 1 -> "100%", null -> "—". */
export function formatPercent(fraction: number | null): string {
  if (fraction == null) {
    return "—";
  }
  const percent = fraction * 100;
  if (percent >= 99.95) {
    return "100%";
  }
  return `${percent.toFixed(1).replace(/\.0$/, "")}%`;
}

/**
 * How many similar entries the entry page fetches up front. The rail and the
 * dialog's first screen work off this fetch; a full response means there may
 * be more, and the dialog keeps loading pages from the similar feed while
 * scrolling.
 */
export const SIMILAR_ENTRIES_FETCH_LIMIT = 100;

/** Best hits first. The backend already orders this way; kept as a guard. */
export function sortedByScore(items: SimilarEntry[]): SimilarEntry[] {
  return [...items].sort((a, b) => b.score - a.score);
}

function numberField(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function stringField(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}
