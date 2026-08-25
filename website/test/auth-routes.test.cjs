require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const { NextRequest } = require("next/server");

const loginRoute = require("../src/app/auth/github/login/route.ts");
const callbackRoute = require("../src/app/auth/github/callback/route.ts");
const {
  authPermissionsCookieName,
  oauthStateCookieName,
  permissionsFromCookie,
  serializeOAuthState,
} = require("../src/lib/auth/session.ts");

test("should redirect github login and persist matching oauth state", async () => {
  const request = new NextRequest("https://app.example/auth/github/login?return_to=/entries/new");

  const response = await loginRoute.GET(request);

  assert.equal(response.status, 307);
  const location = new URL(response.headers.get("location"));
  assert.equal(location.origin + location.pathname, "https://github.com/login/oauth/authorize");
  assert.equal(location.searchParams.get("redirect_uri"), "https://app.example/auth/github/callback");
  assert.equal(location.searchParams.get("scope"), "read:org");
  assert.equal(location.searchParams.get("allow_signup"), "false");

  const cookieState = parseOAuthStateCookie(response.headers.get("set-cookie"));
  assert.equal(cookieState.state, location.searchParams.get("state"));
  assert.equal(cookieState.returnTo, "/entries/new");
});

test("should exchange github callback code and set auth cookies", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    let requestedURL = "";
    let requestedBody = null;
    global.fetch = async (url, init) => {
      requestedURL = String(url);
      requestedBody = JSON.parse(init.body);
      return new Response(
        JSON.stringify({
          access_token: "jwt-token",
          expires_at: "2030-01-02T03:04:05.000Z",
          login: "octocat",
          name: "Octo Cat",
          permissions: ["revisions.approve", "revisions.reject"],
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      );
    };
    const cookie = `${oauthStateCookieName}=${serializeOAuthState({
      state: "state-123",
      returnTo: "/entries/new",
    })}`;
    const request = new NextRequest("https://app.example/auth/github/callback?code=code-123&state=state-123", {
      headers: { cookie },
    });

    const response = await callbackRoute.GET(request);

    assert.equal(response.status, 307);
    assert.equal(response.headers.get("location"), "https://app.example/entries/new");
    assert.equal(requestedURL, "https://backend.example/v1/auth/github/code/exchange");
    assert.deepEqual(requestedBody, {
      code: "code-123",
      redirect_uri: "https://app.example/auth/github/callback",
    });
    const setCookie = response.headers.get("set-cookie");
    assert.match(setCookie, /dynamic_pdb_auth_token=jwt-token/);
    assert.match(setCookie, /dynamic_pdb_auth_login=octocat/);
    assert.match(setCookie, /dynamic_pdb_auth_name=Octo%20Cat/);
    assert.deepEqual(
      permissionsFromCookie(cookieValue(setCookie, authPermissionsCookieName)),
      ["revisions.approve", "revisions.reject"],
    );
    assert.match(setCookie, /dynamic_pdb_auth_state=/);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should redirect callback to invalid_state when state cookie is missing", async () => {
  const request = new NextRequest("https://app.example/auth/github/callback?code=code-123&state=state-123");

  const response = await callbackRoute.GET(request);

  assert.equal(response.status, 307);
  const location = new URL(response.headers.get("location"));
  assert.equal(location.searchParams.get("login_error"), "invalid_state");
});

test("should redirect callback to forbidden when backend returns 403", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    global.fetch = async () =>
      new Response(JSON.stringify({ code: "FORBIDDEN", message: "no org" }), {
        status: 403,
        headers: { "content-type": "application/json" },
      });
    const cookie = `${oauthStateCookieName}=${serializeOAuthState({
      state: "state-123",
      returnTo: "/entries/new",
    })}`;
    const request = new NextRequest("https://app.example/auth/github/callback?code=code-123&state=state-123", {
      headers: { cookie },
    });

    const response = await callbackRoute.GET(request);

    assert.equal(response.status, 307);
    const location = new URL(response.headers.get("location"));
    assert.equal(location.pathname, "/entries/new");
    assert.equal(location.searchParams.get("login_error"), "forbidden");
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should redirect callback to backend_exchange_failed when backend response is incomplete", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    global.fetch = async () =>
      new Response(JSON.stringify({ access_token: "" }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    const cookie = `${oauthStateCookieName}=${serializeOAuthState({
      state: "state-123",
      returnTo: "/entries/new",
    })}`;
    const request = new NextRequest("https://app.example/auth/github/callback?code=code-123&state=state-123", {
      headers: { cookie },
    });

    const response = await callbackRoute.GET(request);

    assert.equal(response.status, 307);
    const location = new URL(response.headers.get("location"));
    assert.equal(location.searchParams.get("login_error"), "backend_exchange_failed");
    assert.match(location.searchParams.get("login_error_detail"), /incomplete token response/);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

function parseOAuthStateCookie(setCookie) {
  const match = setCookie.match(new RegExp(`${oauthStateCookieName}=([^;]+)`));
  assert.ok(match, `missing ${oauthStateCookieName} cookie`);
  return JSON.parse(decodeURIComponent(decodeURIComponent(match[1])));
}

function cookieValue(setCookie, name) {
  const match = setCookie.match(new RegExp(`${name}=([^;]+)`));
  assert.ok(match, `missing ${name} cookie`);
  return decodeURIComponent(match[1]);
}

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}
