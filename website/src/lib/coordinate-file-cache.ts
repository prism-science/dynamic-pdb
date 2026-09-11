import { fetchFileURL } from "@/lib/api/ext";

const MAX_CACHED_FILES = 2;
export const MAX_COORDINATE_FILE_BYTES = 100 * 1024 * 1024;

const coordinateFiles = new Map<string, Promise<string | null>>();

function getCachedCoordinateFile(
  url: string,
): Promise<string | null> | null {
  const cached = coordinateFiles.get(url);
  if (!cached) {
    return null;
  }
  coordinateFiles.delete(url);
  coordinateFiles.set(url, cached);
  return cached;
}

/** Load a coordinate file in the browser, retaining at most two files. */
export function loadCoordinateFile(
  url: string | null,
  signal?: AbortSignal,
): Promise<string | null> {
  if (!url || !/^(https?:|ext:)/i.test(url)) {
    return Promise.resolve(null);
  }

  const cached = getCachedCoordinateFile(url);
  if (cached) {
    return cached;
  }

  const pending = downloadCoordinateFile(url, signal);
  coordinateFiles.set(url, pending);
  trimCache();
  void pending.then((text) => {
    if (text === null && coordinateFiles.get(url) === pending) {
      coordinateFiles.delete(url);
    }
  });
  return pending;
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
    const response = await fetchFileURL(url, {
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

async function downloadCoordinateFile(
  url: string,
  signal?: AbortSignal,
): Promise<string | null> {
  try {
    const response = await fetchFileURL(url, { cache: "no-store", signal });
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
