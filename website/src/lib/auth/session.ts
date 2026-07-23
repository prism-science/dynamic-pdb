import "server-only";

import { cookies } from "next/headers";
import type { NextRequest, NextResponse } from "next/server";

const defaultGithubClientId = "Ov23liRjWaMI9Ur5X57e";
const defaultGithubScope = "read:org";
const authCookiePath = "/";
const oauthStateMaxAgeSeconds = 10 * 60;

export const authTokenCookieName = "dynamic_pdb_auth_token";
export const authLoginCookieName = "dynamic_pdb_auth_login";
export const authNameCookieName = "dynamic_pdb_auth_name";
export const oauthStateCookieName = "dynamic_pdb_auth_state";

export type AuthSession = {
  token: string;
  login: string | null;
  name: string | null;
};

export type OAuthState = {
  state: string;
  returnTo: string;
};

export type PersistedAuth = {
  accessToken: string;
  expiresAt: Date;
  login: string;
  name: string;
};

export async function getAuthSession(): Promise<AuthSession | null> {
  const cookieStore = await cookies();
  const token = cookieStore.get(authTokenCookieName)?.value;
  if (!token) {
    return null;
  }

  return {
    token,
    login: cookieStore.get(authLoginCookieName)?.value ?? null,
    name: cookieStore.get(authNameCookieName)?.value ?? null,
  };
}

export function getGithubClientId(): string {
  return process.env.GITHUB_CLIENT_ID ?? defaultGithubClientId;
}

export function getGithubScope(): string {
  return process.env.GITHUB_SCOPE ?? defaultGithubScope;
}

export function sanitizeReturnTo(value: string | null | undefined): string {
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return "/";
  }
  return value;
}

export function serializeOAuthState(value: OAuthState): string {
  return encodeURIComponent(JSON.stringify(value));
}

export function parseOAuthState(rawValue: string | undefined): OAuthState | null {
  if (!rawValue) {
    return null;
  }

  try {
    const parsed = JSON.parse(
      decodeURIComponent(rawValue),
    ) as Partial<OAuthState>;
    if (typeof parsed.state !== "string") {
      return null;
    }
    return {
      state: parsed.state,
      returnTo: sanitizeReturnTo(parsed.returnTo),
    };
  } catch {
    return null;
  }
}

export function isSecureRequest(request: NextRequest): boolean {
  const forwardedProto = request.headers.get("x-forwarded-proto");
  if (forwardedProto) {
    return forwardedProto.split(",")[0]?.trim() === "https";
  }
  return request.nextUrl.protocol === "https:";
}

export function getPublicOrigin(request: NextRequest): string {
  const configuredOrigin = process.env.APP_BASE_URL;
  if (configuredOrigin) {
    return configuredOrigin.replace(/\/+$/, "");
  }

  const forwardedProto = request.headers.get("x-forwarded-proto");
  const forwardedHost = request.headers.get("x-forwarded-host");
  const protocol =
    forwardedProto?.split(",")[0]?.trim() ||
    request.nextUrl.protocol.replace(/:$/, "");
  const host =
    forwardedHost?.split(",")[0]?.trim() ||
    request.headers.get("host") ||
    request.nextUrl.host;

  return `${protocol}://${host}`;
}

export function setOAuthStateCookie(
  response: NextResponse,
  request: NextRequest,
  value: OAuthState,
): void {
  response.cookies.set({
    name: oauthStateCookieName,
    value: serializeOAuthState(value),
    httpOnly: true,
    sameSite: "lax",
    secure: isSecureRequest(request),
    path: authCookiePath,
    maxAge: oauthStateMaxAgeSeconds,
  });
}

export function clearOAuthStateCookie(response: NextResponse): void {
  clearCookie(response, oauthStateCookieName);
}

export function setAuthCookies(
  response: NextResponse,
  request: NextRequest,
  value: PersistedAuth,
): void {
  const baseCookie = {
    httpOnly: true,
    sameSite: "lax" as const,
    secure: isSecureRequest(request),
    path: authCookiePath,
    expires: value.expiresAt,
  };

  response.cookies.set({
    ...baseCookie,
    name: authTokenCookieName,
    value: value.accessToken,
  });
  response.cookies.set({
    ...baseCookie,
    name: authLoginCookieName,
    value: value.login,
  });
  response.cookies.set({
    ...baseCookie,
    name: authNameCookieName,
    value: value.name,
  });
}

export function clearAuthCookies(response: NextResponse): void {
  clearCookie(response, authTokenCookieName);
  clearCookie(response, authLoginCookieName);
  clearCookie(response, authNameCookieName);
}

function clearCookie(response: NextResponse, name: string): void {
  response.cookies.set({
    name,
    value: "",
    httpOnly: true,
    sameSite: "lax",
    path: authCookiePath,
    expires: new Date(0),
  });
}
