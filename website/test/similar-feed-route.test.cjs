require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { GET } = require("../src/app/entries/[entryId]/similar/route.ts");

test("should page the similar feed with bounded limit and offset", async () => {
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
        json: async () => collectionDocument("similar_entries", [{ entry: { id: "e2" }, score: 0.5, matches: [] }]),
      };
    };

    const response = await GET(
      new Request("http://localhost/entries/entry-1/similar?limit=500&offset=50"),
      { params: Promise.resolve({ entryId: "entry-1" }) },
    );

    assert.equal(
      requestedURL,
      "https://backend.example/v1/entries/entry-1/similar-entries?limit=100&offset=50",
    );
    const body = await response.json();
    assert.deepEqual(body.items, [{ entry: { id: "e2" }, score: 0.5, matches: [] }]);
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
    const body = await response.json();
    assert.deepEqual(body.items, []);
  } finally {
    global.fetch = previousFetch;
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
  }
});
