/**
 * What the /download page says about bulk download (B4): its limits, and the
 * script and curl examples, filled in for one saved selection when the page
 * is opened from the download panel (?selection=<id>).
 *
 * The limits are the backend's defaults (backend/internal/config/config.go);
 * a unit test reads them there, so the page cannot drift from the server.
 */

import { formatDecimalBytes, type FileChoice } from "@/lib/download-selection";

/** Where the website serves the script (website/public/download/). */
export const SCRIPT_FILE_NAME = "dynamic-pdb-download.py";
export const SCRIPT_PATH = `/download/${SCRIPT_FILE_NAME}`;

/** The production API, which the script uses unless told otherwise (its --api default). */
export const PRODUCTION_API_BASE_URL = "https://dynamicpdb.com/api";

/** downloads.max_archive_bytes: one archive's limit, before compression. */
export const MAX_ARCHIVE_BYTES = 10_000_000_000;
/** downloads.archive_ttl: how long a ready archive is kept. */
export const ARCHIVE_DAYS = 7;
/** downloads.rate_limit.requests per downloads.rate_limit.window (a minute), per client. */
export const ARCHIVE_REQUESTS_PER_MINUTE = 10;
/** Files larger than this have no copy in Dynamic PDB's storage: archives link them. */
export const COPY_LIMIT_TEXT = "1 GiB";

/** Shown in the examples when the page was opened without a selection. */
export const SELECTION_PLACEHOLDER = "<selection id>";

const SELECTION_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** The selection ID of ?selection=, or null when it is missing or not a selection ID. */
export function selectionFromQuery(value: string | string[] | undefined): string | null {
  const first = Array.isArray(value) ? value[0] : value;
  if (first === undefined) {
    return null;
  }
  const trimmed = first.trim();
  return SELECTION_ID.test(trimmed) ? trimmed.toLowerCase() : null;
}

/** The /download page's script section, for one saved selection when there is one. */
export function scriptPageHref(selectionId: string | null): string {
  return selectionId === null ? "/download#script" : `/download?selection=${encodeURIComponent(selectionId)}#script`;
}

/**
 * The website's own address when the API is served under it at /api (as on
 * dynamicpdb.com), so the script's download link can be written out in full;
 * null when the API is elsewhere.
 */
export function siteOriginFor(apiBaseUrl: string): string | null {
  let parsed: URL;
  try {
    parsed = new URL(apiBaseUrl);
  } catch {
    return null;
  }
  const path = parsed.pathname.replace(/\/+$/, "");
  if (!path.endsWith("/api")) {
    return null;
  }
  return `${parsed.origin}${path.slice(0, -"/api".length)}`;
}

/** The page's limits, one sentence each. */
export function downloadLimits(): string[] {
  return [
    `One archive holds at most ${formatDecimalBytes(MAX_ARCHIVE_BYTES)} (${MAX_ARCHIVE_BYTES.toLocaleString("en-US")} bytes, before compression). For more, use the script below, or select less.`,
    `Files over ${COPY_LIMIT_TEXT} are linked, not copied: the manifest gives each one's address and SHA-256.`,
    `An archive is kept for ${ARCHIVE_DAYS} days. A saved selection is kept indefinitely, so its ID gives the same files again later.`,
    "Selections, archives and their links need no sign-in: anyone with a link can download it.",
    `Each client may ask for ${ARCHIVE_REQUESTS_PER_MINUTE} archives a minute. Past that the API answers 429 with Retry-After; the script waits and asks again.`,
  ];
}

export type ScriptExamples = {
  /** Fetching and running the script. */
  script: string;
  /** One archive with curl: ask, poll, download, check. */
  archive: string;
  /** Saving a selection without the panel. */
  saveSelection: string;
};

/**
 * The commands the page shows, for the selection when there is one and for
 * the API this website talks to (the script's --api is added when that is not
 * the production API).
 */
export function scriptExamples({
  selectionId,
  apiBaseUrl,
  fileChoice = "all_files",
}: {
  selectionId: string | null;
  apiBaseUrl: string;
  fileChoice?: FileChoice;
}): ScriptExamples {
  const api = apiBaseUrl.replace(/\/+$/, "");
  const selection = selectionId ?? SELECTION_PLACEHOLDER;
  const origin = siteOriginFor(api);
  const run = [`python3 ${SCRIPT_FILE_NAME}`, `--selection ${selection}`, `--files ${fileChoice}`];
  if (api !== PRODUCTION_API_BASE_URL) {
    run.push(`--api ${api}`);
  }
  const script = [
    ...(origin === null ? [] : [`curl -O ${origin}${SCRIPT_PATH}`]),
    run.join(" \\\n  "),
  ].join("\n");
  const zipName = `dynamic-pdb-${selection}-${fileChoice}.zip`;
  const archive = [
    "# 1. Ask for the archive; it is built right away",
    `curl -s -X POST '${api}/v1/downloads' \\`,
    "  -H 'Content-Type: application/vnd.api+json' \\",
    `  -d '{"selection_id": "${selection}",`,
    `       "file_choice": "${fileChoice}", "prepare": true}'`,
    "# 2. Ask again until data.attributes.status is ready",
    "#    (blocked means over the limit: use the script)",
    `curl -s '${api}/v1/downloads/<archive id>'`,
    "# 3. Download data.attributes.archive_url, then check it:",
    "#    shasum must print data.attributes.archive_sha256",
    `curl -o ${zipName} '<archive_url>'`,
    `shasum -a 256 ${zipName}`,
  ].join("\n");
  const saveSelection = [
    `curl -s -X POST '${api}/v1/selections' \\`,
    "  -H 'Content-Type: application/vnd.api+json' \\",
    `  -d '{"model_ids": ["<model id>", "<model id>"]}'`,
    "# data.id is the selection ID",
  ].join("\n");
  return { script, archive, saveSelection };
}
