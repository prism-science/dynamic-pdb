require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ApiRequestError,
  retryAfterSeconds,
  searchFailureFromError,
  searchModels,
} = require("../src/lib/api/entries.ts");
const { GET } = require("../src/app/browse/model-feed/route.ts");
const { readModelSearch } = require("../src/lib/model-search.ts");
const { __setCookieValues } = require("next/headers");
const { authTokenCookieName } = require("../src/lib/auth/session.ts");

const jsonApiMediaType = "application/vnd.api+json";

/** Runs body with fetch replaced by answer and the backend at https://backend.example/. */
async function withBackend(answer, body) {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  const previousInternalURL = process.env.API_INTERNAL_BASE_URL;
  const requests = [];
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    delete process.env.API_INTERNAL_BASE_URL;
    global.fetch = async (url, init) => {
      requests.push({ url: String(url), headers: init.headers });
      return answer(String(url));
    };
    await body(requests);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
    restoreEnv("API_INTERNAL_BASE_URL", previousInternalURL);
  }
}

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}

function searchDocument(ids, meta) {
  return {
    data: ids.map((id) => ({ type: "models", id, attributes: { id, entry_id: "entry-1" } })),
    ...(meta ? { meta } : {}),
  };
}

function jsonResponse(body, init = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": jsonApiMediaType },
    ...init,
  });
}

function errorResponse(status, detail, headers = {}) {
  return new Response(
    JSON.stringify({ errors: [{ status: String(status), code: "X", detail }] }),
    { status, headers: { "content-type": jsonApiMediaType, ...headers } },
  );
}

test("should search models with the search's own query string, then limit", async () => {
  await withBackend(
    () => jsonResponse(searchDocument(["m1", "m2"], { total: { count: 10000, exact: false }, next_cursor: "c2" })),
    async (requests) => {
      const criteria = readModelSearch(new URLSearchParams("model_type=Ensemble&organism=Homo+sapiens&r_free_max=0.20"));

      const page = await searchModels(criteria);

      assert.equal(
        requests[0].url,
        "https://backend.example/v1/models?organism=Homo+sapiens&model_type=Ensemble&r_free_max=0.20&limit=50",
      );
      assert.equal(requests[0].headers.Authorization, undefined);
      assert.deepEqual(page, {
        items: [
          { id: "m1", entry_id: "entry-1" },
          { id: "m2", entry_id: "entry-1" },
        ],
        nextCursor: "c2",
        total: { count: 10000, exact: false },
      });
    },
  );
});

test("should search as the signed-in reader when given a token", async () => {
  await withBackend(
    () => jsonResponse(searchDocument(["m1"], { total: { count: 1, exact: true } })),
    async (requests) => {
      await searchModels(readModelSearch(new URLSearchParams("flagged=true")), { token: "reviewer-token" });

      assert.equal(requests[0].headers.Authorization, "Bearer reviewer-token");
    },
  );
});

test("should read a later page of a search, which has no total", async () => {
  await withBackend(
    () => jsonResponse(searchDocument(["m3"])),
    async (requests) => {
      const page = await searchModels(readModelSearch(new URLSearchParams("flagged=true")), {
        cursor: "c2",
        limit: 20,
      });

      assert.equal(requests[0].url, "https://backend.example/v1/models?flagged=true&limit=20&cursor=c2");
      assert.deepEqual(page, { items: [{ id: "m3", entry_id: "entry-1" }], nextCursor: null, total: null });
    },
  );
});

test("should keep Retry-After and the API's reason on a failed search", async () => {
  await withBackend(
    () => errorResponse(429, "too many searches from this client", { "Retry-After": "7" }),
    async () => {
      const error = await searchModels(readModelSearch(new URLSearchParams("flagged=true"))).catch((caught) => caught);

      assert.ok(error instanceof ApiRequestError);
      assert.equal(error.status, 429);
      assert.equal(error.retryAfterSeconds, 7);
      assert.deepEqual(searchFailureFromError(error), {
        status: 429,
        detail: "too many searches from this client",
        retryAfterSeconds: 7,
      });
    },
  );
});

test("should read Retry-After as whole seconds only", () => {
  assert.equal(retryAfterSeconds("10"), 10);
  assert.equal(retryAfterSeconds(" 3 "), 3);
  assert.equal(retryAfterSeconds("Wed, 21 Oct 2026 07:28:00 GMT"), null);
  assert.equal(retryAfterSeconds("-1"), null);
  assert.equal(retryAfterSeconds(null), null);
  assert.equal(retryAfterSeconds(undefined), null);
});

test("should describe a failure without a reason by its status only", () => {
  assert.deepEqual(searchFailureFromError(new ApiRequestError("Backend request timed out after 20 s", 504)), {
    status: 504,
    detail: null,
    retryAfterSeconds: null,
  });
});

test("should page the model feed with the page's filters and cursor, other parameters left out", async () => {
  await withBackend(
    () => jsonResponse(searchDocument(["m51"], { next_cursor: "c3" })),
    async (requests) => {
      const response = await GET(
        new Request(
          "http://localhost/browse/model-feed?model_type=Ensemble&model_type=Deposited&query=kinase&r_free_max=0.2&cursor=c2&limit=500",
        ),
      );

      assert.equal(
        requests[0].url,
        "https://backend.example/v1/models?model_type=Ensemble&model_type=Deposited&r_free_max=0.2&limit=50&cursor=c2",
      );
      assert.equal(response.status, 200);
      assert.deepEqual(await response.json(), {
        items: [{ id: "m51", entry_id: "entry-1" }],
        nextCursor: "c3",
      });
    },
  );
});

test("should page the model feed as the signed-in reader, so later pages show flags as the first did", async () => {
  __setCookieValues({ [authTokenCookieName]: "reviewer-token" });
  try {
    await withBackend(
      () => jsonResponse(searchDocument(["m51"])),
      async (requests) => {
        await GET(new Request("http://localhost/browse/model-feed?flagged=true&cursor=c2"));

        assert.equal(requests[0].headers.Authorization, "Bearer reviewer-token");
      },
    );
  } finally {
    __setCookieValues({});
  }
});

test("should page the model feed anonymously without a session", async () => {
  await withBackend(
    () => jsonResponse(searchDocument(["m51"])),
    async (requests) => {
      await GET(new Request("http://localhost/browse/model-feed?model_type=Ensemble&cursor=c2"));

      assert.equal(requests[0].headers.Authorization, undefined);
    },
  );
});

test("should pass a failed page's status and Retry-After to the scroll", async () => {
  await withBackend(
    () => errorResponse(504, "the search did not finish within the server's time limit", { "Retry-After": "10" }),
    async () => {
      const response = await GET(new Request("http://localhost/browse/model-feed?source=pdb_archive&cursor=c2"));

      assert.equal(response.status, 504);
      assert.deepEqual(await response.json(), {
        error: {
          status: 504,
          detail: "the search did not finish within the server's time limit",
          retryAfterSeconds: 10,
        },
      });
    },
  );
});

test("should answer 502 when the backend could not be reached", async () => {
  await withBackend(
    () => {
      throw new TypeError("fetch failed");
    },
    async () => {
      const response = await GET(new Request("http://localhost/browse/model-feed?flagged=true&cursor=c2"));

      assert.equal(response.status, 502);
      assert.deepEqual(await response.json(), {
        error: { status: null, detail: null, retryAfterSeconds: null },
      });
    },
  );
});
