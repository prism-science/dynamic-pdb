const extApiBaseURL =
  process.env.NEXT_PUBLIC_EXT_API_BASE_URL ?? "https://extshell.org/api";
const extWebBaseURL =
  process.env.NEXT_PUBLIC_EXT_WEB_BASE_URL ?? "https://extshell.org";

export type ExtExperiment = {
  id: string;
  name: string;
  description: string;
  visibility: "public";
  web_url: string;
};

export type ExtFile = {
  name: string;
  path: string;
  sha256: string;
  size: number;
  directory: boolean;
  available: boolean;
  url?: string;
  error?: string;
  metadata?: Record<string, unknown>;
  directions: Array<"input" | "output">;
};

export type ExtFilePage = {
  items: ExtFile[];
  next_cursor?: string;
};

export type ExtFileReference = {
  experimentId: string;
  path: string;
  sha256: string;
};

type ExtFileRefResponse = {
  path?: string;
  sha256?: string;
};

type ExtFileResponse = {
  ref?: ExtFileRefResponse;
  name?: string;
  path?: string;
  sha256?: string;
  size?: number;
  directory?: boolean;
  error?: string;
  metadata?: Record<string, unknown>;
  url?: string;
};

type ExtFilePageResponse = {
  items?: ExtFileResponse[];
  refs?: Array<{
    direction?: string;
    file_ref?: ExtFileRefResponse;
  }>;
  next_cursor?: string;
};

export async function getExtExperiment(
  experimentId: string,
  signal?: AbortSignal,
): Promise<ExtExperiment> {
  const response = await fetch(
    `${extApiBaseURL.replace(/\/+$/, "")}/v1/experiments/${encodeURIComponent(experimentId)}`,
    {
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal,
    },
  );
  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("This Ext experiment does not exist or is not public.");
    }
    throw new Error(await responseError(response, "Could not load the Ext experiment."));
  }
  const experiment = (await response.json()) as {
    id?: string;
    name?: string;
    description?: string;
    visibility?: string;
  };
  if (
    experiment.id !== experimentId ||
    experiment.visibility !== "public" ||
    typeof experiment.name !== "string"
  ) {
    throw new Error("This Ext experiment does not exist or is not public.");
  }
  return {
    id: experiment.id,
    name: experiment.name,
    description:
      typeof experiment.description === "string" ? experiment.description : "",
    visibility: "public",
    web_url: extExperimentWebURL(experiment.id),
  };
}

export async function searchExtFiles(
  experimentId: string,
  options: {
    query?: string;
    cursor?: string;
    limit?: number;
    signal?: AbortSignal;
  } = {},
): Promise<ExtFilePage> {
  const query = new URLSearchParams();
  const trimmed = options.query?.trim();
  if (trimmed) {
    query.set("q", trimmed);
  }
  if (options.cursor) {
    query.set("cursor", options.cursor);
  }
  query.set("limit", String(options.limit ?? 50));

  const response = await fetch(
    `${extApiBaseURL.replace(/\/+$/, "")}/v1/experiments/${encodeURIComponent(experimentId)}/files?${query}`,
    {
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  if (!response.ok) {
    throw new Error(await responseError(response, "Could not search Ext files."));
  }
  return mapFilePage((await response.json()) as ExtFilePageResponse);
}

export function extFileReferenceURL(
  experimentId: string,
  file: Pick<ExtFile, "path" | "sha256">,
): string {
  const query = new URLSearchParams({ path: file.path });
  if (file.sha256) {
    query.set("sha256", file.sha256);
  }
  return `ext://${experimentId}?${query}`;
}

export function parseExtFileReference(value: string): ExtFileReference | null {
  try {
    const parsed = new URL(value);
    const path = parsed.searchParams.get("path")?.trim() ?? "";
    if (
      parsed.protocol !== "ext:" ||
      !isUUID(parsed.hostname) ||
      path === ""
    ) {
      return null;
    }
    return {
      experimentId: parsed.hostname,
      path,
      sha256: parsed.searchParams.get("sha256")?.trim() ?? "",
    };
  } catch {
    return null;
  }
}

export async function resolveFileURL(value: string): Promise<string> {
  const reference = parseExtFileReference(value);
  if (!reference) {
    return value;
  }

  const cached = resolvedFileURLs.get(value);
  if (cached && cached.expiresAt > Date.now()) {
    return cached.url;
  }
  const pending = pendingFileURLs.get(value);
  if (pending) {
    return pending;
  }

  const resolution = resolveExtFile(reference)
    .then((url) => {
      resolvedFileURLs.set(value, {
        url,
        expiresAt: Date.now() + resolvedFileURLTTL,
      });
      pendingFileURLs.delete(value);
      return url;
    })
    .catch((error) => {
      pendingFileURLs.delete(value);
      throw error;
    });
  pendingFileURLs.set(value, resolution);
  return resolution;
}

export async function fetchFileURL(
  value: string,
  init?: RequestInit,
): Promise<Response> {
  const reference = parseExtFileReference(value);
  if (!reference) {
    return fetch(value, init);
  }

  let resolvedURL = await resolveFileURL(value);
  let response = await fetch(resolvedURL, init);
  if (response.status !== 401 && response.status !== 403) {
    return response;
  }

  resolvedFileURLs.delete(value);
  resolvedURL = await resolveFileURL(value);
  response = await fetch(resolvedURL, init);
  return response;
}

export function extFileKey(file: Pick<ExtFile, "path" | "sha256">): string {
  return `${file.path}\u0000${file.sha256}`;
}

// Accept either a bare experiment UUID or a link to the experiment page, so a
// user can paste whatever they copied out of Ext:
//   6d95a158-…                                     (bare id)
//   https://extshell.org/experiments/6d95a158-…     (web link, any host)
//   extshell.org/experiments/6d95a158-…?tab=files   (no scheme, extra query)
export function parseExtExperimentRef(value: string): string | null {
  // Strip the wrappers a pasted link often arrives in: <…>, "…", '…', and
  // trailing sentence punctuation.
  const trimmed = value
    .trim()
    .replace(/^[<("'\s]+/, "")
    .replace(/[>)"'\s.,;]+$/, "");
  if (trimmed === "") {
    return null;
  }
  if (isUUID(trimmed)) {
    return trimmed.toLowerCase();
  }

  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed)
    ? trimmed
    : `https://${trimmed}`;
  let pathname: string;
  try {
    const parsed = new URL(withScheme);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    pathname = parsed.pathname;
  } catch {
    return null;
  }

  // Require the /experiments/ segment: without it any http(s) URL ending in a
  // UUID would look like an experiment link, whatever the host.
  const segments = pathname.split("/").filter(Boolean);
  const experimentsIndex = segments.lastIndexOf("experiments");
  if (experimentsIndex === -1) {
    return null;
  }
  const candidate = segments[experimentsIndex + 1];
  if (!candidate) {
    return null;
  }
  const decoded = safeDecode(candidate);
  return isUUID(decoded) ? decoded.toLowerCase() : null;
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function extExperimentWebURL(experimentId: string): string {
  return `${extWebBaseURL.replace(/\/+$/, "")}/experiments/${encodeURIComponent(experimentId)}`;
}

export function isUUID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
    value,
  );
}

function mapFilePage(page: ExtFilePageResponse): ExtFilePage {
  const directions = new Map<string, Set<"input" | "output">>();
  for (const ref of page.refs ?? []) {
    const path = ref.file_ref?.path ?? "";
    const sha256 = ref.file_ref?.sha256 ?? "";
    if (ref.direction !== "input" && ref.direction !== "output") {
      continue;
    }
    const key = extFileKey({ path, sha256 });
    const values = directions.get(key) ?? new Set<"input" | "output">();
    values.add(ref.direction);
    directions.set(key, values);
  }

  return {
    items: (page.items ?? []).map((item) => {
      const path = item.path || item.ref?.path || "";
      const sha256 = item.sha256 || item.ref?.sha256 || "";
      const name =
        item.name || path.split("/").filter(Boolean).pop() || "Ext file";
      const size = typeof item.size === "number" ? item.size : 0;
      const directory = item.directory === true;
      const error = typeof item.error === "string" ? item.error : "";
      const url = isHTTPURL(item.url) ? item.url : undefined;
      return {
        name,
        path,
        sha256,
        size,
        directory,
        available: !directory && error === "" && size > 0 && Boolean(url),
        url,
        ...(error ? { error } : {}),
        ...(item.metadata ? { metadata: item.metadata } : {}),
        directions: Array.from(
          directions.get(extFileKey({ path, sha256 })) ?? [],
        ),
      };
    }),
    ...(page.next_cursor ? { next_cursor: page.next_cursor } : {}),
  };
}

async function resolveExtFile(reference: ExtFileReference): Promise<string> {
  const search = reference.sha256 || reference.path;
  let cursor: string | undefined;
  const seenCursors = new Set<string>();
  do {
    const page = await searchExtFiles(reference.experimentId, {
      query: search,
      cursor,
      limit: 500,
    });
    const file = page.items.find(
      (candidate) =>
        candidate.path === reference.path &&
        (!reference.sha256 || candidate.sha256 === reference.sha256),
    );
    if (file?.available && file.url) {
      return file.url;
    }
    cursor = page.next_cursor;
    if (cursor && seenCursors.has(cursor)) {
      break;
    }
    if (cursor) {
      seenCursors.add(cursor);
    }
  } while (cursor);

  throw new Error("This Ext file is no longer available.");
}

function isHTTPURL(value: unknown): value is string {
  if (typeof value !== "string") {
    return false;
  }
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

async function responseError(response: Response, fallback: string): Promise<string> {
  try {
    const body = (await response.json()) as {
      error?: unknown;
      message?: unknown;
    };
    if (typeof body.error === "string" && body.error) {
      return body.error;
    }
    if (typeof body.message === "string" && body.message) {
      return body.message;
    }
  } catch {
    return fallback;
  }
  return fallback;
}

const resolvedFileURLTTL = 4 * 60 * 1000;
const resolvedFileURLs = new Map<string, { url: string; expiresAt: number }>();
const pendingFileURLs = new Map<string, Promise<string>>();
