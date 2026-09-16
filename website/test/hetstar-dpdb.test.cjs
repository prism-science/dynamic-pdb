require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const nextHeaders = require("./stubs/next-headers.cjs");
const {
  hetstarDpdbConfig,
  resolveDpdbConfig,
} = require("../src/lib/hetstar.ts");
const { GET: getFile } = require("../src/app/dpdb-file/route.ts");

test("should call the catalogue directly when the API shares the page's origin", () => {
  const config = resolveDpdbConfig(
    "https://dynamicpdb.com/api",
    "https://dynamicpdb.com",
  );

  assert.equal(config.apiBase, "/api/v1");
});

test("should tolerate a trailing slash on the configured API base", () => {
  const config = resolveDpdbConfig(
    "https://dynamicpdb.com/api/",
    "https://dynamicpdb.com",
  );

  assert.equal(config.apiBase, "/api/v1");
});

test("should proxy the catalogue when the page is served from another origin", () => {
  // `next dev` on localhost against the deployed backend: the API allows only
  // the https://dynamicpdb.com origin, so the browser cannot call it.
  const config = resolveDpdbConfig(
    "https://dynamicpdb.com/api",
    "http://localhost:3000",
  );

  assert.equal(config.apiBase, "/dpdb-api/v1");
});

test("should proxy the catalogue when the origin of the request is unknown", () => {
  const config = resolveDpdbConfig("https://dynamicpdb.com/api", null);

  assert.equal(config.apiBase, "/dpdb-api/v1");
});

test("should proxy the catalogue when the API base is not a usable URL", () => {
  const config = resolveDpdbConfig("/api", "https://dynamicpdb.com");

  assert.equal(config.apiBase, "/dpdb-api/v1");
});

test("should always route artifact downloads through the file proxy", () => {
  // files.dynamicpdb.com is cross-origin from the site either way and sends no
  // CORS headers, so this holds even on dynamicpdb.com itself.
  for (const origin of ["https://dynamicpdb.com", "http://localhost:3000"]) {
    assert.equal(
      resolveDpdbConfig("https://dynamicpdb.com/api", origin).fileProxy,
      "/dpdb-file",
    );
  }
});

test("should read the page origin from the forwarded headers behind the ingress", async () => {
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://dynamicpdb.com/api";
    nextHeaders.__setRequestHeaders({
      host: "dynamic-pdb-website.svc",
      "x-forwarded-proto": "https,http",
      "x-forwarded-host": "dynamicpdb.com,internal",
    });

    const config = await hetstarDpdbConfig();

    assert.equal(config.apiBase, "/api/v1");
  } finally {
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
    nextHeaders.__setRequestHeaders({});
  }
});

test("should fall back to the host header when nothing is forwarded", async () => {
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://dynamicpdb.com/api";
    nextHeaders.__setRequestHeaders({ host: "localhost:3000" });

    const config = await hetstarDpdbConfig();

    assert.equal(config.apiBase, "/dpdb-api/v1");
  } finally {
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
    nextHeaders.__setRequestHeaders({});
  }
});

test("should reject a file proxy request with no target", async () => {
  const response = await getFile(new Request("https://app.example/dpdb-file"));

  assert.equal(response.status, 400);
});

test("should refuse to proxy a host that is not the catalogue's", async () => {
  const target = encodeURIComponent("https://evil.example/payload.cif");

  const response = await getFile(
    new Request(`https://app.example/dpdb-file?u=${target}`),
  );

  assert.equal(response.status, 403);
});

test("should refuse to proxy a plaintext target", async () => {
  const target = encodeURIComponent("http://files.dynamicpdb.com/a.cif");

  const response = await getFile(
    new Request(`https://app.example/dpdb-file?u=${target}`),
  );

  assert.equal(response.status, 403);
});

test("should stream an allowed artifact back without stale encoding headers", async () => {
  const previousFetch = global.fetch;
  try {
    let requested = null;
    global.fetch = async (url, init) => {
      requested = { url: String(url), init };
      return new Response("data_7APT\n", {
        status: 200,
        headers: {
          "content-type": "chemical/x-cif",
          // Node's fetch has already decoded the body; forwarding these would
          // describe something the client is not receiving.
          "content-encoding": "gzip",
          "content-length": "9999",
        },
      });
    };
    const uri = "https://files.dynamicpdb.com/entries/dpdb_kytgultv/model.cif";

    const response = await getFile(
      new Request(`https://app.example/dpdb-file?u=${encodeURIComponent(uri)}`),
    );

    assert.equal(response.status, 200);
    assert.equal(requested.url, uri);
    assert.equal(requested.init.redirect, "follow");
    assert.equal(response.headers.get("content-type"), "chemical/x-cif");
    assert.equal(response.headers.get("content-encoding"), null);
    assert.equal(response.headers.get("content-length"), null);
    assert.equal(response.headers.get("cache-control"), "no-store");
    assert.equal(await response.text(), "data_7APT\n");
  } finally {
    global.fetch = previousFetch;
  }
});

test("should answer 502 rather than throwing when the upstream cannot be reached", async () => {
  const previousFetch = global.fetch;
  try {
    global.fetch = async () => {
      throw new TypeError("fetch failed");
    };
    const uri = "https://files.dynamicpdb.com/entries/dpdb_kytgultv/model.cif";

    const response = await getFile(
      new Request(`https://app.example/dpdb-file?u=${encodeURIComponent(uri)}`),
    );

    assert.equal(response.status, 502);
    assert.equal(await response.text(), "upstream unreachable");
  } finally {
    global.fetch = previousFetch;
  }
});
