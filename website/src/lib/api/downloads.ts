import "server-only";

import { getServerApiBaseUrl, jsonApiMediaType } from "./baseUrl";
import { ApiRequestError, getBackendJSON, postBackendJSON } from "./entries";
import { fetchWithTimeout } from "./timeout";
import type { components } from "./schema.gen";
import {
  selectionFileChecks,
  type CreateSelectionBody,
  type DownloadProblem,
  type DownloadSnapshot,
  type FileChoice,
  type SavedSelection,
  type SelectionModelFileChecks,
} from "@/lib/download-selection";

/**
 * The bulk-download routes as the website server calls them: save a frozen
 * selection (POST /v1/selections), ask for its archive (POST /v1/downloads),
 * read the archive request (GET /v1/downloads/{id}), and whether the server
 * offers them at all. The browser reaches these through the website's server
 * actions and routes, so a reviewer's searches run as the reviewer.
 */

type Schemas = components["schemas"];

/** Saves a selection. Throws ApiRequestError. */
export async function saveSelection(
  body: CreateSelectionBody,
  token?: string,
): Promise<SavedSelection> {
  const document = await postBackendJSON<Schemas["SelectionDocument"]>("/v1/selections", body, token);
  return {
    id: document.data.id,
    modelCount: document.data.attributes.model_count,
    entryCount: document.data.attributes.entry_count,
    createdAt: document.data.attributes.created_at,
  };
}

/**
 * Asks for the archive of a saved selection. prepare false (the panel's way)
 * stops at the preview; prepareArchive then builds it. Repeats return the same
 * archive. Throws ApiRequestError.
 */
export async function requestArchive(
  selectionId: string,
  fileChoice: FileChoice,
  prepare: boolean,
): Promise<DownloadSnapshot> {
  const body: Schemas["CreateDownloadRequest"] = {
    selection_id: selectionId,
    file_choice: fileChoice,
    prepare,
  };
  const document = await postBackendJSON<Schemas["DownloadDocument"]>("/v1/downloads", body);
  return { id: document.data.id, attributes: document.data.attributes };
}

/** Builds an archive whose preview was shown (POST /v1/downloads/{id}/prepare). Throws ApiRequestError. */
export async function prepareArchive(downloadId: string): Promise<DownloadSnapshot> {
  const document = await postBackendJSON<Schemas["DownloadDocument"]>(
    `/v1/downloads/${encodeURIComponent(downloadId)}/prepare`,
    {},
  );
  return { id: document.data.id, attributes: document.data.attributes };
}

/**
 * Asks for a small archive of a ready archive's retryable failures only
 * (POST /v1/downloads/{id}/retry). Throws ApiRequestError.
 */
export async function retryArchive(downloadId: string): Promise<DownloadSnapshot> {
  const document = await postBackendJSON<Schemas["DownloadDocument"]>(
    `/v1/downloads/${encodeURIComponent(downloadId)}/retry`,
    {},
  );
  return { id: document.data.id, attributes: document.data.attributes };
}

export type DownloadFailure = Schemas["DownloadFailureAttributes"];

/** Most failures one page asks for. */
export const FAILURES_PAGE_SIZE = 20;

/** One page of the files a ready archive could not vouch for. Throws ApiRequestError. */
export async function listFailures(
  downloadId: string,
  cursor: string | null,
): Promise<{ items: DownloadFailure[]; nextCursor: string | null }> {
  const params = new URLSearchParams({ limit: String(FAILURES_PAGE_SIZE) });
  if (cursor) {
    params.set("cursor", cursor);
  }
  const document = await getBackendJSON<Schemas["DownloadFailureCollectionDocument"]>(
    `/v1/downloads/${encodeURIComponent(downloadId)}/failures?${params.toString()}`,
  );
  return {
    items: document.data.map((item) => item.attributes),
    nextCursor: document.meta?.next_cursor ?? null,
  };
}

/** Models one page of a saved selection's file checks asks for. */
export const SELECTION_FILES_PAGE_SIZE = 10;

/**
 * One page of a saved selection's models, with each file's last check, for the
 * drawer (researchers' Q10). Throws ApiRequestError.
 */
export async function listSelectionFileChecks(
  selectionId: string,
  cursor: string | null,
): Promise<{ models: SelectionModelFileChecks[]; nextCursor: string | null }> {
  const params = new URLSearchParams({ limit: String(SELECTION_FILES_PAGE_SIZE) });
  if (cursor) {
    params.set("cursor", cursor);
  }
  const document = await getBackendJSON<Schemas["SelectionModelCollectionDocument"]>(
    `/v1/selections/${encodeURIComponent(selectionId)}/models?${params.toString()}`,
  );
  return {
    models: selectionFileChecks(document.data.map((item) => item.attributes)),
    nextCursor: document.meta?.next_cursor ?? null,
  };
}

/** Reads an archive request. Throws ApiRequestError. */
export async function readArchive(downloadId: string): Promise<DownloadSnapshot> {
  const document = await getBackendJSON<Schemas["DownloadDocument"]>(
    `/v1/downloads/${encodeURIComponent(downloadId)}`,
  );
  return { id: document.data.id, attributes: document.data.attributes };
}

/** The JSON:API validation codes a 422 carries for a field (internal/validation). */
const VALIDATION_CODES = new Set([
  "REQUIRED",
  "INVALID_VALUE",
  "INVALID_FORMAT",
  "OUT_OF_RANGE",
  "DUPLICATE",
  "UNKNOWN_FIELD",
  "RESERVED_KEY",
]);

/**
 * A failed call as the panel explains it. Each status maps only the codes the
 * API documents for it; any other code becomes "failed" with the status and
 * code in its detail, so the reader sees exactly what came back.
 */
export function downloadProblem(error: unknown): DownloadProblem {
  if (!(error instanceof ApiRequestError)) {
    return {
      code: "failed",
      detail: error instanceof Error ? error.message : null,
      retryAfterSeconds: null,
    };
  }
  const codes = error.errors.map((item) => item.code).filter((code): code is string => Boolean(code));
  const detail =
    error.errors
      .map((item) => item.detail ?? item.title)
      .filter((text): text is string => Boolean(text?.trim()))
      .join("; ") || null;
  const problem = (code: DownloadProblem["code"]): DownloadProblem => ({
    code,
    detail,
    retryAfterSeconds: error.retryAfterSeconds,
  });
  const only = (...known: string[]) => codes.length > 0 && codes.every((code) => known.includes(code));
  const unexpected = (): DownloadProblem => ({
    code: "failed",
    detail: `The server answered ${error.status ?? "nothing"}${codes.length > 0 ? ` ${codes.join(", ")}` : ""}${
      detail ? `: ${detail}` : ""
    }`,
    retryAfterSeconds: error.retryAfterSeconds,
  });
  switch (error.status) {
    case null:
      return problem("unreachable");
    case 403:
      return only("FORBIDDEN") ? problem("forbidden") : unexpected();
    case 404:
      if (only("SELECTIONS_OFF", "DOWNLOADS_OFF")) {
        return problem("off");
      }
      return only("NOT_FOUND") ? problem("not_found") : unexpected();
    case 409:
      if (only("DOWNLOAD_NOT_PREPARABLE")) {
        return problem("not_preparable");
      }
      if (only("NOTHING_TO_RETRY")) {
        return problem("nothing_to_retry");
      }
      return only("DOWNLOAD_NOT_RETRYABLE") ? problem("not_retryable") : unexpected();
    case 422:
      if (only("SELECTION_EMPTY")) {
        return problem("empty");
      }
      if (only("SELECTION_TOO_LARGE")) {
        return problem("too_large");
      }
      return codes.length > 0 && codes.every((code) => VALIDATION_CODES.has(code)) ? problem("invalid") : unexpected();
    case 429:
      if (only("SELECTION_BUSY")) {
        return problem("selection_busy");
      }
      return only("RATE_LIMITED") ? problem("rate_limited") : unexpected();
    case 503:
      if (only("SELECTIONS_FULL")) {
        return problem("full");
      }
      return only("SERVICE_UNAVAILABLE", "DOWNLOADS_BUSY") ? problem("busy") : unexpected();
    case 504:
      // The website's own fetch timeout comes without a code.
      return codes.length === 0 || only("TIMEOUT") ? problem("timeout") : unexpected();
    default:
      return unexpected();
  }
}

/**
 * Whether POST /v1/downloads refused a selection ID because no saved selection
 * has it (422 INVALID_VALUE at /selection_id).
 */
export function isUnknownSelection(error: unknown): boolean {
  return (
    error instanceof ApiRequestError &&
    error.status === 422 &&
    error.errors.length > 0 &&
    error.errors.every((item) => item.code === "INVALID_VALUE" && item.source?.pointer === "/selection_id")
  );
}

/** Any ID the backend refuses before it reads a table: a probe that costs no query. */
const PROBE_ID = "availability-probe";
const KNOWN_ANSWER_TTL_MS = 60_000;
const UNKNOWN_ANSWER_TTL_MS = 10_000;
/** Pages wait for the answer, so a backend that hangs must not hold them up. */
const PROBE_TIMEOUT_MS = 3_000;

type Availability = "on" | "off" | "unknown";

let cachedAvailability: { available: boolean; until: number } | null = null;

/**
 * Whether the server offers bulk download: both selections and downloads must
 * be on (selections.enabled, downloads.enabled). Off, or with no answer, the
 * website shows no checkboxes and no panel. Asked at most once a minute per
 * website server; a failed answer is asked again after 10 seconds.
 */
export async function bulkDownloadAvailable(now: number = Date.now()): Promise<boolean> {
  if (cachedAvailability !== null && now < cachedAvailability.until) {
    return cachedAvailability.available;
  }
  const [selections, downloads] = await Promise.all([
    routeAvailability(`/v1/selections/${PROBE_ID}`, "SELECTIONS_OFF"),
    routeAvailability(`/v1/downloads/${PROBE_ID}`, "DOWNLOADS_OFF"),
  ]);
  const known = selections !== "unknown" && downloads !== "unknown";
  const available = selections === "on" && downloads === "on";
  cachedAvailability = { available, until: now + (known ? KNOWN_ANSWER_TTL_MS : UNKNOWN_ANSWER_TTL_MS) };
  return available;
}

/** Forgets the cached answer; for tests. */
export function resetBulkDownloadAvailability(): void {
  cachedAvailability = null;
}

/**
 * on: the route answered as a working route does for an unknown ID (404 with
 * code NOT_FOUND). off: 404 with offCode. unknown: anything else, such as a
 * proxy's HTML page.
 */
async function routeAvailability(path: string, offCode: string): Promise<Availability> {
  const baseUrl = getServerApiBaseUrl().replace(/\/+$/, "");
  let response: Response;
  try {
    response = await fetchWithTimeout(
      `${baseUrl}${path}`,
      { cache: "no-store", headers: { Accept: jsonApiMediaType } },
      PROBE_TIMEOUT_MS,
    );
  } catch {
    return "unknown";
  }
  if (response.status !== 404) {
    return "unknown";
  }
  const body: unknown = await response.json().catch(() => null);
  const errors =
    typeof body === "object" && body !== null && Array.isArray((body as { errors?: unknown }).errors)
      ? (body as { errors: unknown[] }).errors
      : [];
  const codes = errors.map((item) =>
    typeof item === "object" && item !== null ? (item as { code?: unknown }).code : undefined,
  );
  if (codes.length === 1 && codes[0] === offCode) {
    return "off";
  }
  return codes.length === 1 && codes[0] === "NOT_FOUND" ? "on" : "unknown";
}

/** The HTTP status the website's own routes answer a problem with. */
export function problemHttpStatus(code: DownloadProblem["code"]): number {
  switch (code) {
    case "off":
    case "not_found":
      return 404;
    case "rate_limited":
    case "selection_busy":
      return 429;
    case "busy":
    case "full":
      return 503;
    case "timeout":
      return 504;
    case "empty":
    case "too_large":
    case "invalid":
      return 422;
    case "forbidden":
      return 403;
    case "not_preparable":
    case "nothing_to_retry":
    case "not_retryable":
      return 409;
    case "unreachable":
    case "failed":
      return 502;
    default: {
      const unknown: never = code;
      throw new Error(`unknown download problem: ${String(unknown)}`);
    }
  }
}

/** Archive request and selection IDs are UUIDs; the website sends nothing else on. */
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isDownloadId(value: string): boolean {
  return UUID_PATTERN.test(value);
}

export function isSelectionId(value: string): boolean {
  return UUID_PATTERN.test(value);
}
