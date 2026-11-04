import type { MetricKey } from "@/lib/api/entries";

import type { ExtractedFile } from "./fileExtraction";
import type { HeaderFill, HeaderFills, ModelDraft, ParsedFile } from "./types";

// The New model form fills R-factors and authors from the header of the
// model's own coordinate file (autoDetect.ts). Each filled value remembers its
// file, so the create request can name the file as the value's source
// (value_sources): the server then records it as read from that file, with the
// file's SHA-256, instead of as typed.
//
// A fill counts only while it still holds: the file is still the model's
// coordinate file, and the field still has exactly the filled value. An edit
// makes the value the depositor's. A file that stops being the model file takes
// its untouched values with it (withoutStaleHeaderFills), because a model's
// scores must come from its own file, never from an input of one of its runs.

/** The scores the browser parser reads from a header (lib/parse/structure.ts). */
export const HEADER_METRIC_KEYS: readonly MetricKey[] = ["r_work", "r_free"];

const AUTHORS_POINTER = "/metadata/authors";

/**
 * The coordinate counts the model takes from its file's own metadata
 * (modelMetadataFromEntity in lib/api/entries.ts), by the pointer the API keys
 * their sources by. They are always read from the file, by the browser or the
 * server, so each one the file's metadata holds is the file's value.
 */
const COMPOSITION_POINTERS: Readonly<Record<string, string>> = {
  atom_count: "/metadata/atom_count",
  modeled_residues: "/metadata/modeled_residues",
  unique_protein_chains: "/metadata/unique_protein_chains",
  unmodeled_fraction: "/metadata/unmodeled_fraction",
};

/**
 * Why a file that names a PDB ID fills no scores (decision D5): its R-factors
 * may be the archive model's, and those are never a new model's.
 */
export function archiveScoresNotice(fileName: string, pdbId: string): string {
  return `${fileName} names PDB ${pdbId}: its scores may be the archive's, so they were not filled. Type the model's own`;
}

export function emptyHeaderFills(): HeaderFills {
  return { metrics: {}, authors: null };
}

/** The fills that still hold, for the badges and the request. */
export function liveHeaderFills(draft: ModelDraft): HeaderFills {
  const fills = draft.headerFills ?? emptyHeaderFills();
  const modelFile = currentModelFile(draft.files);
  const holds = <T>(fill: HeaderFill<T> | null | undefined, value: T): fill is HeaderFill<T> =>
    fill != null && modelFile?.id === fill.fileId && sameValue(fill.value, value);

  const live = emptyHeaderFills();
  for (const key of HEADER_METRIC_KEYS) {
    const fill = fills.metrics[key];
    if (holds(fill, draft.metrics[0]?.values[key] ?? "")) {
      live.metrics[key] = fill;
    }
  }
  if (holds(fills.authors, draft.authors)) {
    live.authors = fills.authors;
  }
  return live;
}

/**
 * The create request's value_sources: each live fill's field, by the JSON
 * pointer the API keys field_sources by, and the model file's artifact id.
 * Undefined when nothing holds, or when the model file has no SHA-256 (the
 * server records a file value only with its checksum).
 */
export function valueSourcesRequest(
  draft: ModelDraft,
): Record<string, { artifact_id: string }> | undefined {
  const modelFile = currentModelFile(draft.files);
  if (!modelFile?.sha256) {
    return undefined;
  }
  const live = liveHeaderFills(draft);
  const sources: Record<string, { artifact_id: string }> = {};
  for (const key of HEADER_METRIC_KEYS) {
    if (live.metrics[key]) {
      sources[`/metrics/${key}`] = { artifact_id: modelFile.id };
    }
  }
  if (live.authors) {
    sources[AUTHORS_POINTER] = { artifact_id: modelFile.id };
  }
  for (const [key, pointer] of Object.entries(COMPOSITION_POINTERS)) {
    if (typeof modelFile.metadata?.[key] === "number") {
      sources[pointer] = { artifact_id: modelFile.id };
    }
  }
  return Object.keys(sources).length > 0 ? sources : undefined;
}

/**
 * The draft with the server's reading of the model file in place of the
 * browser preview's (decision 7): each R-factor and the authors where the
 * field is empty or still holds the preview's value from this file, and the
 * coordinate counts on the file's own metadata. Nothing changes unless the
 * file is still the model's coordinate file. A file that names a PDB ID fills
 * no scores, and the preview's scores from it are cleared (D5).
 */
export function withServerHeader(
  draft: ModelDraft,
  file: { id: string; name: string },
  extracted: ExtractedFile,
): ModelDraft {
  if (currentModelFile(draft.files)?.id !== file.id) {
    return draft;
  }
  const fills = draft.headerFills ?? emptyHeaderFills();
  const fill = <T>(value: T): HeaderFill<T> => ({ fileId: file.id, fileName: file.name, value });
  const nextFills: HeaderFills = { metrics: { ...fills.metrics }, authors: fills.authors };
  const metric = draft.metrics[0] ?? { id: crypto.randomUUID(), values: {} };
  const values = { ...metric.values };
  let notice = draft.headerFillNotice ?? null;

  const serverScores = { r_work: extracted.rWork, r_free: extracted.rFree };
  for (const key of HEADER_METRIC_KEYS) {
    const current = values[key] ?? "";
    const previous = fills.metrics[key];
    const ours = current === "" || (previous?.fileId === file.id && previous.value === current);
    if (!ours) {
      continue;
    }
    const score = serverScores[key as keyof typeof serverScores];
    if (extracted.entryId) {
      delete values[key];
      delete nextFills.metrics[key];
      notice = archiveScoresNotice(file.name, extracted.entryId);
    } else if (score != null) {
      values[key] = String(score);
      nextFills.metrics[key] = fill(String(score));
    }
  }

  let authors = draft.authors;
  const authorsOurs =
    authors.length === 0 ||
    (fills.authors?.fileId === file.id && sameValue(fills.authors.value, authors));
  if (extracted.authors && extracted.authors.length > 0 && authorsOurs) {
    authors = extracted.authors;
    nextFills.authors = fill(extracted.authors);
  }

  const counts: Record<string, number> = {};
  for (const [key, value] of [
    ["atom_count", extracted.atomCount],
    ["modeled_residues", extracted.modeledResidues],
    ["unique_protein_chains", extracted.uniqueProteinChains],
    ["altloc_fraction", extracted.altlocFraction],
  ] as const) {
    if (value != null) {
      counts[key] = value;
    }
  }

  const hasScores = Object.keys(values).length > 0 || draft.metrics.length > 0;
  return {
    ...draft,
    metrics: hasScores ? [{ ...metric, values }, ...draft.metrics.slice(1)] : draft.metrics,
    authors,
    headerFills: nextFills,
    headerFillNotice: notice,
    files: draft.files.map((candidate) =>
      candidate.id === file.id
        ? { ...candidate, metadata: { ...(candidate.metadata ?? {}), ...counts } }
        : candidate,
    ),
  };
}

/**
 * Forgets the fills of a file that is no longer the model's coordinate file
 * (removed, or set to another type), and clears each of its values the
 * depositor left unchanged. headerFillNotice names the file when something
 * was cleared, so the form can say why the fields are empty again.
 */
export function withoutStaleHeaderFills(draft: ModelDraft): ModelDraft {
  const fills = draft.headerFills;
  if (!fills) {
    return draft;
  }
  const modelFile = currentModelFile(draft.files);
  const stale = <T>(fill: HeaderFill<T> | null | undefined): fill is HeaderFill<T> =>
    fill != null && modelFile?.id !== fill.fileId;

  let metrics = draft.metrics;
  let authors = draft.authors;
  const kept = emptyHeaderFills();
  const cleared = new Set<string>();

  for (const key of HEADER_METRIC_KEYS) {
    const fill = fills.metrics[key];
    if (!stale(fill)) {
      if (fill) {
        kept.metrics[key] = fill;
      }
      continue;
    }
    const current = metrics[0];
    if (current && sameValue(fill.value, current.values[key] ?? "")) {
      const values = { ...current.values };
      delete values[key];
      metrics = [{ ...current, values }, ...metrics.slice(1)];
      cleared.add(fill.fileName);
    }
  }
  if (stale(fills.authors)) {
    if (sameValue(fills.authors.value, authors)) {
      authors = [];
      cleared.add(fills.authors.fileName);
    }
  } else {
    kept.authors = fills.authors;
  }

  const unchanged =
    metrics === draft.metrics &&
    authors === draft.authors &&
    sameValue(kept, fills);
  if (unchanged) {
    return draft;
  }
  return {
    ...draft,
    metrics,
    authors,
    headerFills: kept,
    headerFillNotice:
      cleared.size > 0
        ? `Values read from ${[...cleared].join(", ")} were cleared: it is no longer the model file`
        : (draft.headerFillNotice ?? null),
  };
}

/** The badge beside a filled field, such as "from model.cif". */
export function headerFillBadge(fill: HeaderFill<unknown> | null | undefined): string | null {
  return fill ? `from ${fill.fileName}` : null;
}

/** The model's coordinate file: the test isModelFile (helpers.ts) makes, kept
 *  here because helpers.ts builds the request with this module. */
function currentModelFile(files: ParsedFile[]): ParsedFile | null {
  return files.find((file) => file.artifactType === "model") ?? null;
}

function sameValue(first: unknown, second: unknown): boolean {
  return JSON.stringify(first) === JSON.stringify(second);
}
