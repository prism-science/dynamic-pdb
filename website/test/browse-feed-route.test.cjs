require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { GET } = require("../src/app/browse/feed/route.ts");

test("should page the browse feed by cursor with a bounded limit", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    global.fetch = async (url) => {
      requestedURL = String(url);
      return {
        ok: true,
        status: 200,
        json: async () => ({
          data: [{ type: "entries", id: "entry-3", attributes: { id: "entry-3" } }],
          meta: { next_cursor: "cursor-after-3" },
        }),
      };
    };

    const response = await GET(
      new Request("http://localhost/browse/feed?limit=500&cursor=cursor-after-2&query=%20kinase%20"),
    );

    assert.equal(
      requestedURL,
      "https://backend.example/v1/entries?query=kinase&limit=100&cursor=cursor-after-2",
    );
    const body = await response.json();
    assert.deepEqual(body, { items: [{ id: "entry-3" }], nextCursor: "cursor-after-3" });
  } finally {
    global.fetch = previousFetch;
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
  }
});

test("should end the browse feed when the backend rejects the cursor", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    global.fetch = async () => ({ ok: false, status: 400, json: async () => ({}) });

    const response = await GET(new Request("http://localhost/browse/feed?cursor=stale"));

    assert.equal(response.status, 400);
    assert.deepEqual(await response.json(), { items: [], nextCursor: null });
  } finally {
    global.fetch = previousFetch;
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
  }
});
