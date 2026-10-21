require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { GET } = require("../src/app/entries/[entryId]/similar/route.ts");

test("should page the similar feed by cursor with a bounded limit", async () => {
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
          ...collectionDocument("similar_entries", [{ entry: { id: "e2" }, score: 0.5, matches: [] }]),
          meta: { next_cursor: "after-e2" },
        }),
      };
    };

    const response = await GET(
      new Request("http://localhost/entries/entry-1/similar?limit=500&cursor=after-e1"),
      { params: Promise.resolve({ entryId: "entry-1" }) },
    );

    assert.equal(
      requestedURL,
      "https://backend.example/v1/entries/entry-1/similar-entries?limit=100&cursor=after-e1",
    );
    const body = await response.json();
    assert.deepEqual(body, {
      items: [{ entry: { id: "e2" }, score: 0.5, matches: [] }],
      nextCursor: "after-e2",
    });
  } finally {
    global.fetch = previousFetch;
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
  }
});

function collectionDocument(type, items) {
  return {
    data: items.map((attributes) => ({
      type,
      id: attributes.entry.id,
      attributes,
    })),
  };
}

test("should answer with an empty page when the backend fails", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    global.fetch = async () => ({ ok: false, status: 404, json: async () => ({}) });

    const response = await GET(
      new Request("http://localhost/entries/entry-1/similar"),
      { params: Promise.resolve({ entryId: "entry-1" }) },
    );

    assert.equal(response.status, 404);
    assert.deepEqual(await response.json(), { items: [], nextCursor: null });
  } finally {
    global.fetch = previousFetch;
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
  }
});
