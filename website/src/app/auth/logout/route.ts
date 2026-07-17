import { NextRequest, NextResponse } from "next/server";

import {
  clearAuthCookies,
  clearOAuthStateCookie,
  getPublicOrigin,
} from "@/lib/auth/session";

export async function POST(request: NextRequest) {
  const response = NextResponse.redirect(
    new URL("/", `${getPublicOrigin(request)}/`),
    303,
  );
  clearAuthCookies(response);
  clearOAuthStateCookie(response);
  return response;
}
