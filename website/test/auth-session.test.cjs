require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  getGithubClientId,
  getGithubScope,
  getPublicOrigin,
  isSecureRequest,
  parseOAuthState,
  sanitizeReturnTo,
  serializeOAuthState,
} = require("../src/lib/auth/session.ts");

test("should sanitize unsafe return paths", () => {
  assert.equal(sanitizeReturnTo("/entries/new"), "/entries/new");
  assert.equal(sanitizeReturnTo("https://evil.example/entries"), "/");
  assert.equal(sanitizeReturnTo("//evil.example/entries"), "/");
  assert.equal(sanitizeReturnTo(null), "/");
});

test("should parse serialized oauth state and sanitize return_to", () => {
  const serialized = serializeOAuthState({
    state: "state-123",
    returnTo: "/entries/new",
  });

  assert.deepEqual(parseOAuthState(serialized), {
    state: "state-123",
    returnTo: "/entries/new",
  });
  assert.deepEqual(
    parseOAuthState(
      encodeURIComponent(
        JSON.stringify({ state: "state-123", returnTo: "https://evil.example" }),
      ),
    ),
    { state: "state-123", returnTo: "/" },
  );
  assert.equal(parseOAuthState("not-json"), null);
  assert.equal(parseOAuthState(undefined), null);
});

test("should derive secure request from forwarded proto before next url", () => {
  assert.equal(
    isSecureRequest(fakeRequest("http://app.example", { "x-forwarded-proto": "https,http" })),
    true,
  );
  assert.equal(isSecureRequest(fakeRequest("https://app.example")), true);
  assert.equal(isSecureRequest(fakeRequest("http://app.example")), false);
});

test("should derive public origin from config or forwarded headers", () => {
  const previousPublicOrigin = process.env.PUBLIC_APP_ORIGIN;
  const previousAppBaseURL = process.env.APP_BASE_URL;
  try {
    process.env.PUBLIC_APP_ORIGIN = "https://configured.example/";
    delete process.env.APP_BASE_URL;
    assert.equal(getPublicOrigin(fakeRequest("http://ignored.example")), "https://configured.example");

    delete process.env.PUBLIC_APP_ORIGIN;
    process.env.APP_BASE_URL = "https://base.example/";
    assert.equal(getPublicOrigin(fakeRequest("http://ignored.example")), "https://base.example");

    delete process.env.APP_BASE_URL;
    assert.equal(
      getPublicOrigin(
        fakeRequest("http://internal.example", {
          "x-forwarded-proto": "https",
          "x-forwarded-host": "public.example",
        }),
      ),
      "https://public.example",
    );
  } finally {
    restoreEnv("PUBLIC_APP_ORIGIN", previousPublicOrigin);
    restoreEnv("APP_BASE_URL", previousAppBaseURL);
  }
});

test("should read github oauth defaults and env overrides", () => {
  const previousClientID = process.env.GITHUB_CLIENT_ID;
  const previousScope = process.env.GITHUB_SCOPE;
  try {
    delete process.env.GITHUB_CLIENT_ID;
    delete process.env.GITHUB_SCOPE;
    assert.equal(getGithubClientId(), "Ov23liY99dAfuJ1i2fgv");
    assert.equal(getGithubScope(), "read:org");

    process.env.GITHUB_CLIENT_ID = "client-id";
    process.env.GITHUB_SCOPE = "read:user";
    assert.equal(getGithubClientId(), "client-id");
    assert.equal(getGithubScope(), "read:user");
  } finally {
    restoreEnv("GITHUB_CLIENT_ID", previousClientID);
    restoreEnv("GITHUB_SCOPE", previousScope);
  }
});

function fakeRequest(url, headers = {}) {
  return {
    headers: new Headers(headers),
    nextUrl: new URL(url),
  };
}

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}
