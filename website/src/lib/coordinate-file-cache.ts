import { fetchFileURL } from "@/lib/api/ext";

const MAX_CACHED_FILES = 2;
export const MAX_COORDINATE_FILE_BYTES = 100 * 1024 * 1024;
const PROXIED_COORDINATE_HOSTS = new Set([
  "dynamicpdb.com",
  "files.dynamicpdb.com",
]);

/**
 * One coordinate file, being downloaded or already downloaded.
 *
 * The transfer belongs to the cache rather than to whoever asked for it first.
 * The same file is read by the Structure viewer, the Experiment tab and the
 * Sequence tab, and each of them mounts and unmounts on its own -- so a reader
 * that walks away only detaches itself. The transfer is cancelled once the
 * last reader has gone and it has not finished yet; handing one reader's abort
 * straight to a shared download would resolve it as null for everybody else.
 */
type CoordinateFileEntry = {
  /** The shared download. Settles once, whatever any single reader does. */
  promise: Promise<string | null>;
  /** Cancels the transfer itself. Fired only when no reader is left. */
  controller: AbortController;
  /** Readers still waiting on `promise`. */
  readers: number;
  settled: boolean;
  /** A pending "nobody is waiting any more" cancellation, held for a tick so
   *  that a reader which detaches and immediately re-attaches keeps the
   *  download -- which is exactly what React does to every effect in
   *  development, where Strict Mode runs mount, cleanup and mount again. */
  release: ReturnType<typeof setTimeout> | null;
};

const coordinateFiles = new Map<string, CoordinateFileEntry>();

/** Load a coordinate file in the browser, retaining at most two files. */
export function loadCoordinateFile(
  url: string | null,
  signal?: AbortSignal,
): Promise<string | null> {
  if (!url || !/^(https?:|ext:)/i.test(url)) {
    return Promise.resolve(null);
  }
  if (signal?.aborted) {
    return Promise.resolve(null);
  }

  return attach(url, acquire(url), signal);
}

/** Load a binary structure file in the browser without retaining it. */
export async function loadCoordinateBytes(
  url: string | null,
  signal?: AbortSignal,
): Promise<Uint8Array<ArrayBuffer> | null> {
  if (!url || !/^(https?:|ext:)/i.test(url)) {
    return null;
  }

  try {
    const response = await fetchFileURL(coordinateRequestURL(url), {
      cache: "no-store",
      signal,
    });
    if (!response.ok) {
      throw new Error(`${response.status} ${response.statusText}`);
    }
    return await readBoundedBytes(response);
  } catch (error) {
    if (!signal?.aborted) {
      console.error("read binary structure file failed", url, error);
    }
    return null;
  }
}

/** The cache entry for this file, started if it is not there yet. */
function acquire(url: string): CoordinateFileEntry {
  const cached = coordinateFiles.get(url);
  if (cached) {
    // Re-insert to keep the map in least-recently-used order.
    coordinateFiles.delete(url);
    coordinateFiles.set(url, cached);
    if (cached.release !== null) {
      clearTimeout(cached.release);
      cached.release = null;
    }
    return cached;
  }

  const controller = new AbortController();
  const entry: CoordinateFileEntry = {
    promise: Promise.resolve(null),
    controller,
    readers: 0,
    settled: false,
    release: null,
  };
  entry.promise = downloadCoordinateFile(url, controller.signal).then(
    (text) => {
      entry.settled = true;
      // A file we could not read is not worth keeping: the next reader should
      // try again rather than be handed the failure for the whole session.
      if (text === null && coordinateFiles.get(url) === entry) {
        coordinateFiles.delete(url);
      }
      return text;
    },
  );
  coordinateFiles.set(url, entry);
  trimCache();
  return entry;
}

/**
 * Wait on a shared download on behalf of one reader.
 *
 * Aborting `signal` drops this reader -- it resolves null and stops listening
 * -- and leaves the download alone for as long as anyone else still wants it.
 */
function attach(
  url: string,
  entry: CoordinateFileEntry,
  signal?: AbortSignal,
): Promise<string | null> {
  if (!signal) {
    return entry.promise;
  }

  entry.readers += 1;
  return new Promise<string | null>((resolve) => {
    let done = false;
    const finish = (text: string | null) => {
      if (done) {
        return;
      }
      done = true;
      signal.removeEventListener("abort", onAbort);
      release(url, entry);
      resolve(text);
    };
    const onAbort = () => finish(null);
    signal.addEventListener("abort", onAbort);
    void entry.promise.then(finish, () => finish(null));
  });
}

function release(url: string, entry: CoordinateFileEntry): void {
  entry.readers -= 1;
  if (entry.readers > 0 || entry.settled || entry.release !== null) {
    return;
  }
  entry.release = setTimeout(() => {
    entry.release = null;
    if (entry.readers > 0 || entry.settled) {
      return;
    }
    entry.controller.abort();
    if (coordinateFiles.get(url) === entry) {
      coordinateFiles.delete(url);
    }
  }, 0);
}

async function downloadCoordinateFile(
  url: string,
  signal?: AbortSignal,
): Promise<string | null> {
  try {
    const response = await fetchFileURL(coordinateRequestURL(url), {
      cache: "no-store",
      signal,
    });
    if (!response.ok) {
      throw new Error(`${response.status} ${response.statusText}`);
    }
    return await readBoundedText(response);
  } catch (error) {
    if (!signal?.aborted) {
      console.error("read model coordinates failed", url, error);
    }
    return null;
  }
}

function coordinateRequestURL(url: string): string {
  try {
    const parsed = new URL(url);
    if (
      parsed.protocol === "https:" &&
      PROXIED_COORDINATE_HOSTS.has(parsed.host)
    ) {
      return `/dpdb-file?u=${encodeURIComponent(url)}`;
    }
  } catch {
    // Ext references and relative URLs are resolved by fetchFileURL itself.
  }
  return url;
}

async function readBoundedText(response: Response): Promise<string | null> {
  const contentLength = Number(response.headers.get("content-length") ?? "0");
  if (
    Number.isFinite(contentLength) &&
    contentLength > MAX_COORDINATE_FILE_BYTES
  ) {
    await response.body?.cancel();
    return null;
  }

  if (!response.body) {
    const text = await response.text();
    return text.length <= MAX_COORDINATE_FILE_BYTES ? text : null;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const chunks: string[] = [];
  let bytesRead = 0;

  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    bytesRead += value.byteLength;
    if (bytesRead > MAX_COORDINATE_FILE_BYTES) {
      await reader.cancel();
      return null;
    }
    chunks.push(decoder.decode(value, { stream: true }));
  }
  chunks.push(decoder.decode());
  return chunks.join("");
}

async function readBoundedBytes(
  response: Response,
): Promise<Uint8Array<ArrayBuffer> | null> {
  const contentLength = Number(response.headers.get("content-length") ?? "0");
  if (
    Number.isFinite(contentLength) &&
    contentLength > MAX_COORDINATE_FILE_BYTES
  ) {
    await response.body?.cancel();
    return null;
  }

  if (!response.body) {
    const buffer = await response.arrayBuffer();
    return buffer.byteLength <= MAX_COORDINATE_FILE_BYTES
      ? new Uint8Array(buffer)
      : null;
  }

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let bytesRead = 0;

  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    bytesRead += value.byteLength;
    if (bytesRead > MAX_COORDINATE_FILE_BYTES) {
      await reader.cancel();
      return null;
    }
    chunks.push(value);
  }

  const bytes = new Uint8Array(bytesRead);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return bytes;
}

function trimCache(): void {
  while (coordinateFiles.size > MAX_CACHED_FILES) {
    const oldest = coordinateFiles.keys().next().value;
    if (oldest === undefined) {
      return;
    }
    coordinateFiles.delete(oldest);
  }
}
