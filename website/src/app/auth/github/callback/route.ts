import { NextRequest, NextResponse } from "next/server";

import { getApiBaseUrl, jsonApiMediaType } from "@/lib/api/baseUrl";
import {
  clearOAuthStateCookie,
  getPublicOrigin,
  oauthStateCookieName,
  parseOAuthState,
  setAuthCookies,
} from "@/lib/auth/session";

type BackendTokenResponse = {
  access_token: string;
  expires_at: string;
  login: string;
  name: string;
  permissions: string[];
};

type BackendErrorResponse = {
  code?: string;
  message?: string;
};

type LoginErrorCode =
  | "access_denied"
  | "backend_exchange_failed"
  | "forbidden"
  | "github_exchange_failed"
  | "invalid_state"
  | "missing_code";

type Failure = {
  ok: false;
  error: LoginErrorCode;
  detail?: string;
};

type Result<T> = { ok: true; value: T } | Failure;

export async function GET(request: NextRequest) {
  const githubError = request.nextUrl.searchParams.get("error");
  if (githubError) {
    return redirectToError(request, "access_denied");
  }

  const oauthState = parseOAuthState(
    request.cookies.get(oauthStateCookieName)?.value,
  );
  const returnedState = request.nextUrl.searchParams.get("state");
  if (!oauthState || !returnedState || oauthState.state !== returnedState) {
    return redirectToError(request, "invalid_state");
  }

  const code = request.nextUrl.searchParams.get("code");
  if (!code) {
    return redirectToError(request, "missing_code", oauthState.returnTo);
  }

  const backendTokenResult = await exchangeBackendToken(request, code);
  if (!backendTokenResult.ok) {
    return redirectToError(
      request,
      backendTokenResult.error,
      oauthState.returnTo,
      backendTokenResult.detail,
    );
  }

  const response = NextResponse.redirect(
    new URL(oauthState.returnTo, `${getPublicOrigin(request)}/`),
  );
  clearOAuthStateCookie(response);
  setAuthCookies(response, request, {
    accessToken: backendTokenResult.value.access_token,
    expiresAt: new Date(backendTokenResult.value.expires_at),
    login: backendTokenResult.value.login,
    name: backendTokenResult.value.name,
    permissions: backendTokenResult.value.permissions,
  });
  return response;
}

async function exchangeBackendToken(
  request: NextRequest,
  code: string,
): Promise<Result<BackendTokenResponse>> {
  const callbackUrl = new URL(
    "/auth/github/callback",
    `${getPublicOrigin(request)}/`,
  );

  let response: Response;
  try {
    response = await fetch(`${getApiBaseUrl()}/v1/auth/github/code/exchange`, {
      method: "POST",
      headers: {
        Accept: jsonApiMediaType,
        "Content-Type": jsonApiMediaType,
      },
      body: JSON.stringify({
        code,
        redirect_uri: callbackUrl.toString(),
      }),
      cache: "no-store",
    });
  } catch {
    return {
      ok: false,
      error: "backend_exchange_failed",
      detail: "Could not reach backend auth code exchange endpoint.",
    };
  }

  let bodyText = "";
  try {
    bodyText = await response.text();
  } catch {
    bodyText = "";
  }
  const detail = formatBackendErrorDetail(response.status, bodyText);

  if (response.status === 403) {
    return { ok: false, error: "forbidden" };
  }

  if (response.status === 401) {
    return { ok: false, error: "github_exchange_failed", detail };
  }

  if (!response.ok) {
    return { ok: false, error: "backend_exchange_failed", detail };
  }

  try {
    const payload = JSON.parse(bodyText) as BackendTokenResponse;
    if (
      typeof payload.access_token !== "string" ||
      payload.access_token.length === 0 ||
      typeof payload.expires_at !== "string" ||
      payload.expires_at.length === 0 ||
      typeof payload.login !== "string" ||
      payload.login.length === 0 ||
      typeof payload.name !== "string" ||
      !Array.isArray(payload.permissions) ||
      !payload.permissions.every(
        (permission) => typeof permission === "string",
      )
    ) {
      return {
        ok: false,
        error: "backend_exchange_failed",
        detail: "Backend returned an incomplete token response.",
      };
    }
    return { ok: true, value: payload };
  } catch {
    return {
      ok: false,
      error: "backend_exchange_failed",
      detail: "Backend returned a non-JSON token response.",
    };
  }
}

function redirectToError(
  request: NextRequest,
  error: LoginErrorCode,
  returnTo = "/",
  detail?: string,
) {
  const redirectUrl = new URL(returnTo, `${getPublicOrigin(request)}/`);
  redirectUrl.searchParams.set("login_error", error);
  if (detail) {
    redirectUrl.searchParams.set("login_error_detail", detail.slice(0, 500));
  }

  const response = NextResponse.redirect(redirectUrl);
  clearOAuthStateCookie(response);
  return response;
}

function formatBackendErrorDetail(status: number, bodyText: string): string {
  if (!bodyText) {
    return `status=${status}`;
  }

  try {
    const payload = JSON.parse(bodyText) as BackendErrorResponse;
    const parts = [
      `status=${status}`,
      payload.code ? `code=${payload.code}` : null,
      payload.message ? `message=${payload.message}` : null,
    ].filter(Boolean);
    return parts.join("; ");
  } catch {
    return `status=${status}; body=${bodyText.slice(0, 400)}`;
  }
}
