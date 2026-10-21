require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const { NextRequest } = require("next/server");

const nextHeaders = require("./stubs/next-headers.cjs");
const { ApiRequestError, getEntry } = require("../src/lib/api/entries.ts");
const {
  backendReadTimeoutMs,
  backendWriteTimeoutMs,
} = require("../src/lib/api/timeout.ts");
const { proxyUploadControlRequest } = require("../src/lib/api/uploadProxy.ts");
const { authTokenCookieName } = require("../src/lib/auth/session.ts");
const { proxyUpstream } = require("../src/lib/dpdb-proxy.ts");

test("should throw a 504 ApiRequestError and log it when the backend sends no response in time", async (t) => {
  // given
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const consoleError = t.mock.method(console, "error", () => {});
  const backend = fetchThatNeverAnswers();
  useFetchForTest(t, backend.fetch);
  useApiBaseURLForTest(t, "https://backend.example/");

  // when
  const pending = getEntry(undefined, "entry-1");
  const { init } = await backend.called;
  t.mock.timers.tick(backendReadTimeoutMs - 1);
  const abortedBeforeDeadline = init.signal.aborted;
  t.mock.timers.tick(1);

  // then
  assert.equal(abortedBeforeDeadline, false);
  await assert.rejects(pending, (error) => {
    assert.ok(error instanceof ApiRequestError);
    assert.equal(error.status, 504);
    assert.match(error.message, /timed out after 20 s/);
    return true;
  });
  assert.deepEqual(consoleError.mock.calls[0].arguments, [
    "upstream request timed out",
    {
      method: "GET",
      url: "https://backend.example/v1/entries/entry-1",
      timeoutMs: backendReadTimeoutMs,
    },
  ]);
});

test("should return 504 from the upload proxy when the backend sends no response in time", async (t) => {
  // given
  t.mock.timers.enable({ apis: ["setTimeout"] });
  t.mock.method(console, "error", () => {});
  const backend = fetchThatNeverAnswers();
  useFetchForTest(t, backend.fetch);
  useApiBaseURLForTest(t, "https://backend.example/");
  nextHeaders.__setCookieValues({ [authTokenCookieName]: "jwt-token" });
  const request = new NextRequest("https://app.example/files/complete", {
    method: "POST",
    body: "{}",
  });

  // when
  const pending = proxyUploadControlRequest(request, "/v1/files/complete");
  await backend.called;
  t.mock.timers.tick(backendWriteTimeoutMs);
  const response = await pending;

  // then
  assert.equal(response.status, 504);
  assert.deepEqual(await response.json(), {
    error: "Backend upload endpoint timed out.",
  });
});

test("should return 504 from the file proxy when the upstream sends no response in time", async (t) => {
  // given
  t.mock.timers.enable({ apis: ["setTimeout"] });
  t.mock.method(console, "error", () => {});
  const upstream = fetchThatNeverAnswers();
  useFetchForTest(t, upstream.fetch);

  // when
  const pending = proxyUpstream("https://files.example/model.cif");
  await upstream.called;
  t.mock.timers.tick(backendReadTimeoutMs);
  const response = await pending;

  // then
  assert.equal(response.status, 504);
  assert.equal(await response.text(), "upstream timed out");
});

test("should keep streaming the proxied body when the timeout passes after the headers arrived", async (t) => {
  // given
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let upstreamSignal = null;
  let bodyController = null;
  const body = new ReadableStream({
    start(controller) {
      bodyController = controller;
    },
  });
  useFetchForTest(t, async (_url, init) => {
    upstreamSignal = init.signal;
    return new Response(body, { status: 200 });
  });

  // when
  const response = await proxyUpstream("https://files.example/model.cif");
  t.mock.timers.tick(backendReadTimeoutMs * 2);
  bodyController.enqueue(new TextEncoder().encode("data_model"));
  bodyController.close();

  // then
  assert.equal(upstreamSignal.aborted, false);
  assert.equal(response.status, 200);
  assert.equal(await response.text(), "data_model");
});

/** A fetch stub that only settles when its signal aborts, as a hung backend would. */
function fetchThatNeverAnswers() {
  let markCalled;
  const called = new Promise((resolve) => {
    markCalled = resolve;
  });
  const fetch = (url, init) => {
    markCalled({ url: String(url), init });
    return new Promise((_resolve, reject) => {
      init.signal.addEventListener("abort", () => {
        reject(new DOMException("This operation was aborted", "AbortError"));
      });
    });
  };
  return { fetch, called };
}

function useFetchForTest(t, fetch) {
  const previousFetch = global.fetch;
  global.fetch = fetch;
  t.after(() => {
    global.fetch = previousFetch;
  });
}

function useApiBaseURLForTest(t, value) {
  const previous = process.env.NEXT_PUBLIC_API_BASE_URL;
  process.env.NEXT_PUBLIC_API_BASE_URL = value;
  t.after(() => {
    if (previous === undefined) {
      delete process.env.NEXT_PUBLIC_API_BASE_URL;
    } else {
      process.env.NEXT_PUBLIC_API_BASE_URL = previous;
    }
  });
}
