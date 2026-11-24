import type { components } from "@/lib/api/schema.gen";
import { entryIdentity } from "@/lib/entry-label";
import { modelTitle } from "@/lib/model-metrics";
import {
  MODEL_SEARCH_FILTERS,
  filterParameters,
  readModelSearch,
  type ModelSearchCriteria,
  type ModelSearchFilter,
} from "@/lib/model-search";

/**
 * The working selection of the bulk download: what a reader has picked on
 * /browse and on entry pages, kept in the browser until it is downloaded
 * (decision 7). It names models three ways, as POST /v1/selections takes them:
 * models picked one by one, whole searches (every result, every page) and
 * models left out. Saving it freezes it on the server; downloads only use
 * saved selections.
 *
 * Everything here is plain data and pure functions, so the rules can be tested
 * without a browser; the panel's components hold the state.
 */

type Schemas = components["schemas"];

export type FileChoice = Schemas["DownloadFileChoice"];
export type DownloadAttributes = Schemas["DownloadAttributes"];
export type DownloadCounts = Schemas["DownloadCounts"];
export type CreateSelectionBody = Schemas["CreateSelectionRequest"];

/** POST /v1/selections limits, as openapi.yaml documents them. */
export const MAX_PICKED_MODELS = 10_000;
export const MAX_WHOLE_SEARCHES = 10;
export const MAX_LEFT_OUT_MODELS = 10_000;

/** Where the browser keeps the working selection. */
export const SELECTION_STORAGE_KEY = "dynamic-pdb.download-selection";
const SELECTION_STORAGE_VERSION = 1;

/** A selection POST /v1/selections saved. */
export type SavedSelection = {
  id: string;
  modelCount: number;
  entryCount: number;
  createdAt: string;
};

/** An archive request as GET /v1/downloads/{id} returns it. */
export type DownloadSnapshot = {
  id: string;
  attributes: DownloadAttributes;
};

export type StartDownloadInput = {
  /** The working selection as POST /v1/selections takes it. */
  selection: CreateSelectionBody;
  /** A selection saved from this same working selection, to download again without saving. */
  savedSelectionId: string | null;
  fileChoice: FileChoice;
};

/**
 * started: the archive request. failed: why, as the panel explains it. Either
 * way, saved is the selection when this call saved one, so the panel keeps it
 * and a retry reuses it instead of saving the same selection again.
 */
export type StartDownloadResult =
  | { status: "started"; saved: SavedSelection | null; download: DownloadSnapshot }
  | { status: "failed"; saved: SavedSelection | null; problem: DownloadProblem };

/** The answer to building a planned archive or retrying a ready one's failed files. */
export type ArchiveActionResult =
  | { status: "done"; download: DownloadSnapshot }
  | { status: "failed"; problem: DownloadProblem };

/** A model as the panel lists it: enough to name it without asking the API. */
export type SelectedModel = {
  id: string;
  title: string;
  entryId: string;
  entryLabel: string;
};

/** A search result as the selection lists it. */
export function selectedModelFromResult(result: Schemas["ModelSearchAttributes"]): SelectedModel {
  const identity = result.entry ? entryIdentity(result.entry) : { name: null, id: result.entry_id };
  return {
    id: result.id,
    title: modelTitle(result),
    entryId: result.entry_id,
    entryLabel: identity.name ? `${identity.name} (${identity.id})` : identity.id,
  };
}

/** Every result of one search, as GET /v1/models runs it. */
export type WholeSearch = {
  /** The search's query string, as modelSearchQuery writes it. */
  query: string;
  /** The filters in words, for the review list. */
  label: string;
  /** The total the search showed when it was picked. */
  count: number;
  /** False when the total was capped ("10,000+"). */
  countExact: boolean;
};

/** A selection saved from this working selection, reusable while it is unchanged. */
export type FrozenSelection = {
  id: string;
  /** selectionFingerprint of the working selection it was saved from. */
  fingerprint: string;
  modelCount: number;
  entryCount: number;
  createdAt: string;
};

/** The archive the panel is following, and the retries of its failed files, oldest first. */
export type TrackedDownload = {
  id: string;
  selectionId: string;
  fileChoice: FileChoice;
  retryIds: string[];
};

export type WorkingSelection = {
  picked: SelectedModel[];
  searches: WholeSearch[];
  leftOut: SelectedModel[];
  addDepositedModels: boolean;
  frozen: FrozenSelection | null;
  download: TrackedDownload | null;
};

/** Which of the API's limits a change would break. */
export type SelectionLimit = "picked" | "searches" | "left_out";

export type SelectionChange = { selection: WorkingSelection } | { limit: SelectionLimit };

export function emptySelection(): WorkingSelection {
  return {
    picked: [],
    searches: [],
    leftOut: [],
    addDepositedModels: false,
    frozen: null,
    download: null,
  };
}

/** Nothing picked and no whole search: there is nothing to save. */
export function isEmptySelection(selection: WorkingSelection): boolean {
  return selection.picked.length === 0 && selection.searches.length === 0;
}

export function hasWholeSearch(selection: WorkingSelection, query: string): boolean {
  return selection.searches.some((search) => search.query === query);
}

/**
 * Whether a model's checkbox shows checked. searchQuery is the search the
 * model is listed in, or null outside a search (an entry page). A model of a
 * whole search shows checked on that search's results; elsewhere only a model
 * picked by itself does, since the browser cannot know every search's results.
 */
export function isModelChecked(
  selection: WorkingSelection,
  modelId: string,
  searchQuery: string | null,
): boolean {
  if (selection.leftOut.some((model) => model.id === modelId)) {
    return false;
  }
  if (selection.picked.some((model) => model.id === modelId)) {
    return true;
  }
  return searchQuery !== null && hasWholeSearch(selection, searchQuery);
}

/**
 * Checks or unchecks one model. On the results of a whole search, unchecking
 * leaves the model out (exclude_model_ids) and checking takes it back;
 * elsewhere checking picks it and unchecking removes it.
 */
export function setModelChecked(
  selection: WorkingSelection,
  model: SelectedModel,
  checked: boolean,
  searchQuery: string | null,
): SelectionChange {
  const inWholeSearch = searchQuery !== null && hasWholeSearch(selection, searchQuery);
  const picked = selection.picked.filter((item) => item.id !== model.id);
  const leftOut = selection.leftOut.filter((item) => item.id !== model.id);
  if (checked) {
    if (inWholeSearch) {
      return { selection: { ...selection, leftOut } };
    }
    if (picked.length >= MAX_PICKED_MODELS) {
      return { limit: "picked" };
    }
    return { selection: { ...selection, picked: [...picked, model], leftOut } };
  }
  if (inWholeSearch) {
    if (leftOut.length >= MAX_LEFT_OUT_MODELS) {
      return { limit: "left_out" };
    }
    return { selection: { ...selection, picked, leftOut: [...leftOut, model] } };
  }
  return { selection: { ...selection, picked } };
}

/** Selects every result of a search, however many pages it has. */
export function addWholeSearch(selection: WorkingSelection, search: WholeSearch): SelectionChange {
  if (hasWholeSearch(selection, search.query)) {
    return { selection };
  }
  if (selection.searches.length >= MAX_WHOLE_SEARCHES) {
    return { limit: "searches" };
  }
  return { selection: { ...selection, searches: [...selection.searches, search] } };
}

export function removeWholeSearch(selection: WorkingSelection, query: string): WorkingSelection {
  return { ...selection, searches: selection.searches.filter((search) => search.query !== query) };
}

export function removePicked(selection: WorkingSelection, modelId: string): WorkingSelection {
  return { ...selection, picked: selection.picked.filter((model) => model.id !== modelId) };
}

/** Takes a left-out model back into the selection. */
export function removeLeftOut(selection: WorkingSelection, modelId: string): WorkingSelection {
  return { ...selection, leftOut: selection.leftOut.filter((model) => model.id !== modelId) };
}

export function withAddDepositedModels(
  selection: WorkingSelection,
  addDepositedModels: boolean,
): WorkingSelection {
  return { ...selection, addDepositedModels };
}

export function withFrozen(selection: WorkingSelection, frozen: FrozenSelection): WorkingSelection {
  return { ...selection, frozen };
}

export function withDownload(
  selection: WorkingSelection,
  download: TrackedDownload | null,
): WorkingSelection {
  return { ...selection, download };
}

/** Adds a retry archive to the archive being followed (once, in the order asked). */
export function withRetry(selection: WorkingSelection, retryId: string): WorkingSelection {
  const download = selection.download;
  if (download === null || download.id === retryId || download.retryIds.includes(retryId)) {
    return selection;
  }
  return { ...selection, download: { ...download, retryIds: [...download.retryIds, retryId] } };
}

/** Empties the selection. The archive being followed stays: it is the reader's. */
export function clearedSelection(selection: WorkingSelection): WorkingSelection {
  return { ...emptySelection(), download: selection.download };
}

export function limitMessage(limit: SelectionLimit): string {
  switch (limit) {
    case "picked":
      return `A selection can hold at most ${MAX_PICKED_MODELS.toLocaleString("en-US")} models picked one by one. Select all results of a search instead.`;
    case "searches":
      return `A selection can hold at most ${MAX_WHOLE_SEARCHES} whole searches. Remove one first.`;
    case "left_out":
      return `A selection can leave out at most ${MAX_LEFT_OUT_MODELS.toLocaleString("en-US")} models. Narrow the search instead.`;
    default:
      return unknownValue("selection limit", limit);
  }
}

/** The POST /v1/selections body of a working selection. */
export function selectionRequest(selection: WorkingSelection): CreateSelectionBody {
  return {
    model_ids: selection.picked.map((model) => model.id),
    searches: selection.searches.map((search) => search.query),
    exclude_model_ids: selection.leftOut.map((model) => model.id),
    add_deposited_models: selection.addDepositedModels,
  };
}

/** What the saved selection depends on, in a fixed order, so equal selections compare equal. */
export function selectionFingerprint(selection: WorkingSelection): string {
  const request = selectionRequest(selection);
  return JSON.stringify([
    [...(request.model_ids ?? [])].sort(),
    [...(request.searches ?? [])].sort(),
    [...(request.exclude_model_ids ?? [])].sort(),
    request.add_deposited_models,
  ]);
}

/** The saved selection to download again, while the working selection is unchanged. */
export function reusableSelectionId(selection: WorkingSelection): string | null {
  if (selection.frozen === null) {
    return null;
  }
  return selection.frozen.fingerprint === selectionFingerprint(selection) ? selection.frozen.id : null;
}

/**
 * How many models the selection holds, as far as the browser can tell. Whole
 * searches count as they did when picked, and searches may overlap each other
 * and the picked models, so with any whole search (or the Deposited option) it
 * is an estimate; saving gives the exact count.
 */
export type SelectionSize = {
  models: number;
  approximate: boolean;
  /** A whole search's total was capped: the selection holds more than models. */
  atLeast: boolean;
};

export function selectionSize(selection: WorkingSelection): SelectionSize {
  const leftOutIds = new Set(selection.leftOut.map((model) => model.id));
  const pickedKept = selection.picked.filter((model) => !leftOutIds.has(model.id)).length;
  if (selection.searches.length === 0) {
    // Only picked models: left-out models count only where they were picked.
    return { models: pickedKept, approximate: selection.addDepositedModels, atLeast: false };
  }
  const capped = selection.searches.filter((search) => !search.countExact);
  if (capped.length > 0) {
    // Searches may overlap, so only the largest capped search is sure: it holds
    // more than its capped count, less any left-out models it may contain.
    const largest = Math.max(...capped.map((search) => search.count));
    return { models: Math.max(0, largest - selection.leftOut.length), approximate: true, atLeast: true };
  }
  const fromSearches = selection.searches.reduce((sum, search) => sum + search.count, 0);
  return {
    models: Math.max(0, pickedKept + fromSearches - selection.leftOut.length),
    approximate: true,
    atLeast: false,
  };
}

/** "1 model", "12 models", "about 1,234 models", "more than 10,000 models". */
export function selectionSizeText(size: SelectionSize): string {
  const count = size.models.toLocaleString("en-US");
  const noun = size.models === 1 ? "model" : "models";
  if (size.atLeast) {
    // A whole search the API did not count.
    return size.models === 0 ? "an uncounted number of models" : `more than ${count} ${noun}`;
  }
  return size.approximate ? `about ${count} ${noun}` : `${count} ${noun}`;
}

/** The filters of a search in words: "Model type: Ensemble · R-free ≤ 0.2". */
export function wholeSearchLabel(criteria: ModelSearchCriteria): string {
  const parts: string[] = [];
  for (const filter of MODEL_SEARCH_FILTERS) {
    const part = filterPhrase(filter, criteria);
    if (part !== null) {
      parts.push(part);
    }
  }
  return parts.length > 0 ? parts.join(" · ") : "Every model";
}

function filterPhrase(filter: ModelSearchFilter, criteria: ModelSearchCriteria): string | null {
  const valuesOf = (parameter: string) =>
    criteria.filter(([name]) => name === parameter).map(([, value]) => value);
  switch (filter.kind) {
    case "text_list": {
      const values = valuesOf(filter.parameter);
      return values.length > 0 ? `${filter.label}: ${values.join(", ")}` : null;
    }
    case "choice_list": {
      const values = valuesOf(filter.parameter).map(
        (value) => filter.choices.find((choice) => choice.value === value)?.label ?? value,
      );
      return values.length > 0 ? `${filter.label}: ${values.join(", ")}` : null;
    }
    case "range": {
      const [minimum] = valuesOf(filter.minParameter);
      const [maximum] = valuesOf(filter.maxParameter);
      const unit = filter.unit ? ` ${filter.unit}` : "";
      if (minimum !== undefined && maximum !== undefined) {
        return `${filter.label} ${minimum}–${maximum}${unit}`;
      }
      if (minimum !== undefined) {
        return `${filter.label} ≥ ${minimum}${unit}`;
      }
      if (maximum !== undefined) {
        return `${filter.label} ≤ ${maximum}${unit}`;
      }
      return null;
    }
    case "yes_no": {
      const [value] = valuesOf(filter.parameter);
      if (value === undefined) {
        return null;
      }
      return value === "true" ? filter.yesLabel : value === "false" ? filter.noLabel : `${filter.label}: ${value}`;
    }
    case "option": {
      const [value] = valuesOf(filter.parameter);
      return value === "true" ? filter.label : null;
    }
    default:
      return unknownValue("model search filter kind", (filter as { kind: never }).kind);
  }
}

/** The whole search of a page's query string, labelled. */
export function wholeSearchFromQuery(query: string, count: number, countExact: boolean): WholeSearch {
  return {
    query,
    label: wholeSearchLabel(readModelSearch(new URLSearchParams(query))),
    count,
    countExact,
  };
}

/**
 * Reads the stored working selection. Anything unreadable (another version, a
 * hand-edited value, a broken write) is dropped: problem "unreadable" lets the
 * panel say so instead of starting empty without a word.
 */
export function parseStoredSelection(raw: string | null): {
  selection: WorkingSelection;
  problem: "none" | "unreadable";
} {
  if (raw === null) {
    return { selection: emptySelection(), problem: "none" };
  }
  try {
    const value: unknown = JSON.parse(raw);
    const stored = storedSelection(value);
    if (stored !== null) {
      return { selection: stored, problem: "none" };
    }
  } catch {
    // Falls through to unreadable.
  }
  return { selection: emptySelection(), problem: "unreadable" };
}

export function serializeSelection(selection: WorkingSelection): string {
  return JSON.stringify({ version: SELECTION_STORAGE_VERSION, ...selection });
}

function storedSelection(value: unknown): WorkingSelection | null {
  if (!isRecord(value) || value.version !== SELECTION_STORAGE_VERSION) {
    return null;
  }
  const picked = listOf(value.picked, selectedModel);
  const searches = listOf(value.searches, wholeSearch);
  const leftOut = listOf(value.leftOut, selectedModel);
  const frozen = value.frozen === null ? null : frozenSelection(value.frozen);
  const download = value.download === null ? null : trackedDownload(value.download);
  if (
    picked === null ||
    searches === null ||
    leftOut === null ||
    typeof value.addDepositedModels !== "boolean" ||
    (value.frozen !== null && frozen === null) ||
    (value.download !== null && download === null) ||
    picked.length > MAX_PICKED_MODELS ||
    searches.length > MAX_WHOLE_SEARCHES ||
    leftOut.length > MAX_LEFT_OUT_MODELS
  ) {
    return null;
  }
  return { picked, searches, leftOut, addDepositedModels: value.addDepositedModels, frozen, download };
}

function selectedModel(value: unknown): SelectedModel | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.id) ||
    typeof value.title !== "string" ||
    !isNonEmptyString(value.entryId) ||
    typeof value.entryLabel !== "string"
  ) {
    return null;
  }
  return { id: value.id, title: value.title, entryId: value.entryId, entryLabel: value.entryLabel };
}

function wholeSearch(value: unknown): WholeSearch | null {
  if (
    !isRecord(value) ||
    typeof value.query !== "string" ||
    typeof value.label !== "string" ||
    !isCount(value.count) ||
    typeof value.countExact !== "boolean"
  ) {
    return null;
  }
  return { query: value.query, label: value.label, count: value.count, countExact: value.countExact };
}

function frozenSelection(value: unknown): FrozenSelection | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.id) ||
    typeof value.fingerprint !== "string" ||
    !isCount(value.modelCount) ||
    !isCount(value.entryCount) ||
    typeof value.createdAt !== "string"
  ) {
    return null;
  }
  return {
    id: value.id,
    fingerprint: value.fingerprint,
    modelCount: value.modelCount,
    entryCount: value.entryCount,
    createdAt: value.createdAt,
  };
}

function trackedDownload(value: unknown): TrackedDownload | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.id) ||
    !isNonEmptyString(value.selectionId) ||
    !isFileChoice(value.fileChoice) ||
    // Archives stored before retries were tracked have no list: none yet.
    (value.retryIds !== undefined && (!Array.isArray(value.retryIds) || !value.retryIds.every(isNonEmptyString)))
  ) {
    return null;
  }
  return {
    id: value.id,
    selectionId: value.selectionId,
    fileChoice: value.fileChoice,
    retryIds: Array.isArray(value.retryIds) ? [...value.retryIds] : [],
  };
}

function listOf<T>(value: unknown, item: (value: unknown) => T | null): T[] | null {
  if (!Array.isArray(value)) {
    return null;
  }
  const items: T[] = [];
  for (const entry of value) {
    const parsed = item(entry);
    if (parsed === null) {
      return null;
    }
    items.push(parsed);
  }
  return items;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim() !== "";
}

function isCount(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

/** The file choices of POST /v1/downloads, in the order the panel offers them. */
export const FILE_CHOICES: readonly FileChoice[] = [
  "original_mmcif",
  "modified_coordinates",
  "all_files",
];

export function isFileChoice(value: unknown): value is FileChoice {
  return typeof value === "string" && (FILE_CHOICES as readonly string[]).includes(value);
}

/** A file choice in words: the label and what the archive then holds. */
export function fileChoiceText(choice: FileChoice): { label: string; description: string } {
  switch (choice) {
    case "original_mmcif":
      return {
        label: "Original mmCIF",
        description:
          "The PDB's coordinate file, byte for byte, of each selected model that holds it: usually the Deposited models. Computed models (for example qFit or ensembles) do not hold it.",
      };
    case "modified_coordinates":
      return {
        label: "Modified coordinate files",
        description: "The coordinate files of the selected Dynamic PDB models.",
      };
    case "all_files":
      return {
        label: "All available files",
        description:
          "Every file of the selected models and their entries: coordinates, structure factors, logs, sequences and other files.",
      };
    default:
      return unknownValue("file choice", choice);
  }
}

/**
 * The reuse terms, exactly as the researchers worded them (item 10). A request,
 * not a condition.
 */
export const REUSE_TERMS =
  "These data are dedicated to the public domain under CC0 1.0. We ask that you acknowledge Dynamic PDB and cite the depositors of the entries you use.";
export const REUSE_LICENSE_URL = "https://creativecommons.org/publicdomain/zero/1.0/";

/** Where an archive stands, as the panel shows it. */
export type DownloadView =
  | { kind: "in_progress"; step: string; preview: PreviewSummary | null }
  /** Planned and waiting for the reader: the preview, then "Build the archive". */
  | { kind: "preview"; preview: PreviewSummary }
  | {
      kind: "ready";
      archiveUrl: string;
      manifestUrl: string | null;
      archiveBytes: number | null;
      archiveSha256: string | null;
      expiresAt: string | null;
      counts: DownloadCounts | null;
      /** Failed files a retry may fetch again (read_failed). */
      retryableFiles: number;
      /** Set on a retry: the archive whose failed files it fetches again. */
      retryOf: string | null;
    }
  /** Over the size limit, so never built; the preview says what it would hold. */
  | { kind: "blocked"; message: string; preview: PreviewSummary | null; maxArchiveBytes: number }
  /** retryOf: set on a retry, whose parent can be asked to retry its failed files again. */
  | { kind: "failed"; message: string; retryOf: string | null }
  | { kind: "expired"; message: string; retryOf: string | null };

/** Whether the panel keeps asking for news of an archive. */
export function downloadInProgress(view: DownloadView): boolean {
  return view.kind === "in_progress";
}

/**
 * The panel's view of an archive request. Throws for a status this website
 * does not know, so an API change can never read as a finished download.
 */
export function downloadView(attributes: DownloadAttributes): DownloadView {
  const preview = attributes.preview ? previewSummary(attributes.preview, attributes.max_archive_bytes) : null;
  switch (attributes.status) {
    case "queued":
      return { kind: "in_progress", step: "Waiting for its turn", preview };
    case "planning":
      return { kind: "in_progress", step: "Adding up the files", preview };
    case "planned":
      if (attributes.prepare_requested) {
        return { kind: "in_progress", step: "Waiting to be built", preview };
      }
      if (preview === null) {
        throw new Error("a planned archive came without its preview");
      }
      return { kind: "preview", preview };
    case "preparing":
      return { kind: "in_progress", step: "Writing the zip", preview };
    case "ready":
      if (!attributes.archive_url) {
        throw new Error("a ready archive came without archive_url");
      }
      return {
        kind: "ready",
        archiveUrl: attributes.archive_url,
        manifestUrl: attributes.manifest_url ?? null,
        archiveBytes: attributes.archive_bytes ?? null,
        archiveSha256: attributes.archive_sha256 ?? null,
        expiresAt: attributes.expires_at ?? null,
        counts: attributes.counts ?? null,
        retryableFiles: attributes.retryable_files ?? 0,
        retryOf: attributes.retry_of ?? null,
      };
    case "blocked":
      return {
        kind: "blocked",
        message:
          attributes.message ??
          `The files add up to more than ${formatDecimalBytes(attributes.max_archive_bytes)}, the most one archive may hold.`,
        preview,
        maxArchiveBytes: attributes.max_archive_bytes,
      };
    case "failed":
      return {
        kind: "failed",
        message: attributes.message ?? "The archive could not be built.",
        retryOf: attributes.retry_of ?? null,
      };
    case "expired":
      return {
        kind: "expired",
        message: attributes.message ?? "The archive was offered for 7 days and has expired.",
        retryOf: attributes.retry_of ?? null,
      };
    default:
      return unknownValue("download status", attributes.status);
  }
}

export type DownloadPreview = Schemas["DownloadPreview"];
export type DownloadSize = Schemas["DownloadSize"];
export type DownloadReasonCode = Schemas["DownloadReasonCode"];
export type DownloadFileAvailability = Schemas["DownloadFileAvailability"];
export type ArtifactType = Schemas["ArtifactType"];

/** L1's preview in words, for the drawer. */
export type PreviewSummary = {
  /** "12 models in 4 entries · 37 files". */
  headline: string;
  modelsWithoutFiles: number;
  byType: { type: ArtifactType; label: string; files: number; size: string }[];
  /** What the 10 GB limit measures: the files the zip takes, before compression. */
  included: string;
  /** What the zip will be at most. */
  archive: string;
  /** Files the zip lists but does not hold. */
  linked: string;
  leftOut: { code: DownloadReasonCode; status: DownloadFileAvailability; files: number; text: string }[];
  verification: { verified: number; mismatch: number; notVerified: number };
  relatedDatasets: string | null;
  overLimit: boolean;
  /** The size the limit is checked against, when it is known. */
  includedBytes: number | null;
  maxArchiveBytes: number;
};

export function previewSummary(preview: DownloadPreview, maxArchiveBytes: number): PreviewSummary {
  const records = preview.records;
  const related = preview.related_datasets;
  return {
    headline: [
      `${countWords(records.models, "model")} in ${countWords(records.entries, "entry", "entries")}`,
      countWords(preview.files.total, "file"),
    ].join(" · "),
    modelsWithoutFiles: records.models_without_matching_files,
    byType: preview.files.by_type.map((item) => ({
      type: item.type,
      label: artifactTypeLabel(item.type),
      files: item.files,
      size: sizeText(item.size),
    })),
    included: sizeText(preview.sizes.included),
    archive: sizeText(preview.sizes.archive),
    linked: sizeText(preview.sizes.linked),
    leftOut: preview.files.not_included.map((item) => ({
      code: item.code,
      status: item.status,
      files: item.files,
      text: reasonCodeText(item.code),
    })),
    verification: {
      verified: preview.files.verification.verified,
      mismatch: preview.files.verification.mismatch,
      notVerified: preview.files.verification.not_verified,
    },
    relatedDatasets:
      related.total === 0
        ? null
        : `${countWords(related.total, "related dataset")}, linked in manifest.tsv, never in the zip`,
    overLimit: preview.over_limit,
    includedBytes: preview.sizes.included.kind === "exact" ? (preview.sizes.included.bytes ?? null) : null,
    maxArchiveBytes,
  };
}

/** A size and how sure it is, in words. */
export function sizeText(size: DownloadSize): string {
  const unknownFiles =
    size.files_size_unknown && size.files_size_unknown > 0
      ? `, plus ${countWords(size.files_size_unknown, "file")} of unknown size`
      : "";
  switch (size.kind) {
    case "exact":
      return `${formatDecimalBytes(size.bytes ?? 0)}${unknownFiles}`;
    case "estimate":
      return `about ${formatDecimalBytes(size.bytes ?? 0)}${size.tolerance ? ` (${size.tolerance})` : ""}${unknownFiles}`;
    case "unknown":
      return "unknown: no size is recorded";
    default:
      return unknownValue("size kind", size.kind);
  }
}

export function artifactTypeLabel(type: ArtifactType): string {
  switch (type) {
    case "model":
      return "Coordinate files";
    case "structure_factors":
      return "Structure factors";
    case "fasta":
      return "Sequences";
    case "other":
      return "Other files";
    default:
      return unknownValue("file type", type);
  }
}

/** Why the archive leaves a file out, in plain words. */
export function reasonCodeText(code: DownloadReasonCode): string {
  switch (code) {
    case "over_copy_limit":
      return "Over 1 GiB: linked, not copied";
    case "copy_failed":
      return "Linked: copying it into Dynamic PDB's storage failed";
    case "copy_off":
      return "Linked: Dynamic PDB keeps no copy of it";
    case "not_checked_yet":
      return "Linked: its checksum is not checked yet";
    case "copied_after_planning":
      return "Linked: copied after this archive was planned; ask again to include it";
    case "stored_file_differs":
      return "Unavailable: the stored file differs from its record";
    case "changed_at_source":
      return "Changed at the source: the source serves other bytes and no copy was kept";
    case "source_unreadable":
      return "Unavailable: the source could not be read";
    case "copy_not_in_storage":
      return "Unavailable: no copy in Dynamic PDB's storage";
    case "copy_missing":
      return "Unavailable: Dynamic PDB's copy is missing";
    case "copy_differs":
      return "Unavailable: Dynamic PDB's copy differs from the record";
    case "read_failed":
      return "Unavailable: Dynamic PDB's copy could not be read; a retry may fetch it";
    default:
      return unknownValue("reason code", code);
  }
}

/** A manifest availability in words. */
export function availabilityText(status: DownloadFileAvailability): string {
  switch (status) {
    case "included":
      return "included";
    case "linked":
      return "linked";
    case "unavailable":
      return "unavailable";
    case "changed-at-source":
      return "changed at the source";
    default:
      return unknownValue("file availability", status);
  }
}

function countWords(count: number, singular: string, plural = `${singular}s`): string {
  return `${count.toLocaleString("en-US")} ${count === 1 ? singular : plural}`;
}

/** Each manifest status in words, for the counts of a ready archive. */
export function countLines(counts: DownloadCounts): { key: keyof DownloadCounts; count: number; text: string }[] {
  const keys: (keyof DownloadCounts)[] = ["included", "linked", "unavailable", "changed_at_source"];
  return keys.map((key) => ({ key, count: counts[key], text: countText(key) }));
}

function countText(key: keyof DownloadCounts): string {
  switch (key) {
    case "included":
      return "in the zip, each checked against its recorded SHA-256";
    case "linked":
      return "linked: no copy here (for example over 1 GiB); manifest.tsv gives the source and the recorded SHA-256";
    case "unavailable":
      return "unavailable: cannot be read in their recorded form";
    case "changed_at_source":
      return "changed at the source: the source no longer serves the recorded bytes, and no copy was kept";
    default:
      return unknownValue("download count", key);
  }
}

/** Bytes in decimal units, as the 10 GB limit is (10 GB = 10,000,000,000 bytes). */
export function formatDecimalBytes(bytes: number): string {
  const units = ["bytes", "kB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit += 1;
  }
  if (unit === 0) {
    return bytes === 1 ? "1 byte" : `${bytes} bytes`;
  }
  const rounded = value >= 100 ? Math.round(value) : Math.round(value * 10) / 10;
  return `${rounded} ${units[unit]}`;
}

function unknownValue(what: string, value: never): never {
  throw new Error(`unknown ${what}: ${String(value)}`);
}

/** Why saving the selection, asking for its archive or reading the archive did not work. */
export type DownloadProblemCode =
  | "off"
  | "empty"
  | "too_large"
  | "forbidden"
  | "invalid"
  | "not_found"
  | "rate_limited"
  | "selection_busy"
  | "busy"
  | "full"
  | "timeout"
  | "unreachable"
  | "not_preparable"
  | "nothing_to_retry"
  | "not_retryable"
  | "failed";

export type DownloadProblem = {
  code: DownloadProblemCode;
  /** The API's own words, when it sent any. */
  detail: string | null;
  retryAfterSeconds: number | null;
};

/** A problem as the panel says it: a heading and what to do. */
export function problemMessage(problem: DownloadProblem): { title: string; body: string } {
  const wait = waitPhrase(problem.retryAfterSeconds);
  switch (problem.code) {
    case "off":
      return { title: "Bulk download is turned off", body: "This server does not offer bulk download right now." };
    case "empty":
      return {
        title: "Nothing to download",
        body: "No published model matches the selection any more. Change the selection and try again.",
      };
    case "too_large":
      return {
        title: "The selection is too large",
        body: problem.detail ?? "More models matched than one selection may hold. Narrow the searches.",
      };
    case "forbidden":
      return {
        title: "A search uses a filter only reviewers may use",
        body: problem.detail ?? "Remove that search, or sign in as a reviewer.",
      };
    case "invalid":
      return { title: "The selection could not be saved", body: problem.detail ?? "The API refused the request." };
    case "not_found":
      return { title: "The archive is gone", body: "No archive request has this ID any more. Start the download again." };
    case "rate_limited":
      return { title: "Too many requests", body: `Try again ${wait}.` };
    case "selection_busy":
      return {
        title: "Archives of this selection are still being built",
        body: `Wait for one to finish, then try again ${wait}.`,
      };
    case "busy":
      return { title: "The server is busy", body: `It is building as many archives as it takes. Try again ${wait}.` };
    case "full":
      return {
        title: "No space for new selections",
        body: "The space for saved selections is used up. Try again later.",
      };
    case "timeout":
      return {
        title: "Saving took too long",
        body: `Narrow the searches, for example with a model type or a resolution range, and try again ${wait}.`,
      };
    case "unreachable":
      return { title: "The server could not be reached", body: "Check your connection and try again." };
    case "not_preparable":
      return {
        title: "This archive can no longer be built",
        body: problem.detail ?? "It is blocked, failed or expired. Check the archive again to plan a new one.",
      };
    case "nothing_to_retry":
      return {
        title: "No failed file can be fetched again",
        body: "A retry fetches only files whose stored copy could not be read; the others are listed with their reason.",
      };
    case "not_retryable":
      return {
        title: "This archive cannot be retried",
        body: problem.detail ?? "Only a ready archive that has not expired can be retried.",
      };
    case "failed":
      return { title: "Something went wrong", body: problem.detail ?? "Try again in a moment." };
    default:
      return unknownValue("download problem", problem.code);
  }
}

function waitPhrase(seconds: number | null): string {
  if (seconds === null || seconds <= 0) {
    return "in a few seconds";
  }
  return seconds === 1 ? "in 1 second" : `in ${seconds} seconds`;
}
