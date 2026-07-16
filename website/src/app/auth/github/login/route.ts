import { NextRequest, NextResponse } from "next/server";

import {
  getGithubClientId,
  getGithubScope,
  getPublicOrigin,
  sanitizeReturnTo,
  setOAuthStateCookie,
} from "@/lib/auth/session";

export async function GET(request: NextRequest) {
  const state = crypto.randomUUID();
  const returnTo = sanitizeReturnTo(
    request.nextUrl.searchParams.get("return_to"),
  );
  const callbackUrl = new URL(
    "/auth/github/callback",
    `${getPublicOrigin(request)}/`,
  );
  const authorizeUrl = new URL("https://github.com/login/oauth/authorize");

  authorizeUrl.searchParams.set("client_id", getGithubClientId());
  authorizeUrl.searchParams.set("redirect_uri", callbackUrl.toString());
  authorizeUrl.searchParams.set("scope", getGithubScope());
  authorizeUrl.searchParams.set("state", state);
  authorizeUrl.searchParams.set("allow_signup", "false");

  const response = NextResponse.redirect(authorizeUrl);
  setOAuthStateCookie(response, request, { state, returnTo });
  return response;
}
