/** Longer than the backend's 15 s deadline on GET requests, so the backend's
 *  own error response, which it logs with a request id, arrives first. */
export const backendReadTimeoutMs = 20_000;

/** The backend's http.Server WriteTimeout: it has given up on a write by then. */
export const backendWriteTimeoutMs = 180_000;

export class FetchTimeoutError extends Error {
  readonly timeoutMs: number;

  constructor(method: string, url: string, timeoutMs: number) {
    super(`${method} ${url} sent no response within ${timeoutMs} ms`);
    this.name = "FetchTimeoutError";
    this.timeoutMs = timeoutMs;
  }
}

/** fetch that gives up when no response headers arrive within timeoutMs. The
 *  body is not timed, so a large file keeps streaming once it has started. */
export async function fetchWithTimeout(
  url: string,
  init: Omit<RequestInit, "signal">,
  timeoutMs: number,
): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(url, { ...init, signal: controller.signal });
  } catch (error) {
    if (controller.signal.aborted) {
      const method = init.method ?? "GET";
      console.error("upstream request timed out", { method, url, timeoutMs });
      throw new FetchTimeoutError(method, url, timeoutMs);
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
}
