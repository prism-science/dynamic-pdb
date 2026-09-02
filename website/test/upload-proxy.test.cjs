require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const { NextRequest } = require("next/server");

const nextHeaders = require("./stubs/next-headers.cjs");
const { proxyUploadControlRequest } = require("../src/lib/api/uploadProxy.ts");
const { authTokenCookieName } = require("../src/lib/auth/session.ts");

const jsonApiMediaType = "application/vnd.api+json";

test("should return 401 when upload proxy has no auth session", async () => {
  nextHeaders.__setCookieValues({});
  const request = new NextRequest("https://app.example/files", {
    method: "POST",
    body: "{}",
  });

  const response = await proxyUploadControlRequest(request, "/v1/files");

  assert.equal(response.status, 401);
  assert.deepEqual(await response.json(), {
    error: "You must be signed in to upload files.",
  });
});

test("should forward upload control request with bearer token and content type", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    nextHeaders.__setCookieValues({ [authTokenCookieName]: "jwt-token" });
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let forwarded = null;
    global.fetch = async (url, init) => {
      forwarded = { url: String(url), init };
      return new Response(JSON.stringify({ ok: true }), {
        status: 202,
        headers: { "content-type": jsonApiMediaType },
      });
    };
    const request = new NextRequest("https://app.example/files", {
      method: "POST",
      headers: { "content-type": "text/plain" },
      body: JSON.stringify({ filename: "model.cif" }),
    });

    const response = await proxyUploadControlRequest(request, "/v1/files");

    assert.equal(response.status, 202);
    assert.equal(await response.text(), JSON.stringify({ ok: true }));
    assert.equal(forwarded.url, "https://backend.example/v1/files");
    assert.equal(forwarded.init.method, "POST");
    assert.equal(forwarded.init.headers.Accept, jsonApiMediaType);
    assert.equal(forwarded.init.headers.Authorization, "Bearer jwt-token");
    assert.equal(forwarded.init.headers["Content-Type"], jsonApiMediaType);
    assert.equal(forwarded.init.body, JSON.stringify({ filename: "model.cif" }));
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
    nextHeaders.__setCookieValues({});
  }
});

test("should return 204 when backend upload control endpoint returns 204", async () => {
  const previousFetch = global.fetch;
  try {
    nextHeaders.__setCookieValues({ [authTokenCookieName]: "jwt-token" });
    global.fetch = async () => new Response(null, { status: 204 });
    const request = new NextRequest("https://app.example/files/abort", {
      method: "POST",
      body: JSON.stringify({ key: "key", upload_id: "upload-id" }),
    });

    const response = await proxyUploadControlRequest(request, "/v1/files/abort");

    assert.equal(response.status, 204);
    assert.equal(await response.text(), "");
  } finally {
    global.fetch = previousFetch;
    nextHeaders.__setCookieValues({});
  }
});

test("should return 502 when backend upload control endpoint is unreachable", async () => {
  const previousFetch = global.fetch;
  try {
    nextHeaders.__setCookieValues({ [authTokenCookieName]: "jwt-token" });
    global.fetch = async () => {
      throw new Error("network down");
    };
    const request = new NextRequest("https://app.example/files", {
      method: "POST",
      body: "{}",
    });

    const response = await proxyUploadControlRequest(request, "/v1/files");

    assert.equal(response.status, 502);
    assert.deepEqual(await response.json(), {
      error: "Could not reach backend upload endpoint.",
    });
  } finally {
    global.fetch = previousFetch;
    nextHeaders.__setCookieValues({});
  }
});

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}
