import { createHmac } from "crypto";
import { NextRequest, NextResponse } from "next/server";

import { demoUserId } from "@/lib/api/entries";
import { sanitizeReturnTo, setAuthCookies } from "@/lib/auth/session";

const demoLogin = "local-demo-user";
const demoName = "Local Demo User";
const tokenTTLSeconds = 60 * 60 * 24 * 30;

export function GET(request: NextRequest) {
  if (process.env.NODE_ENV === "production") {
    return new NextResponse(null, { status: 404 });
  }

  const returnTo = sanitizeReturnTo(request.nextUrl.searchParams.get("return_to"));
  const expiresAt = new Date(Date.now() + tokenTTLSeconds * 1000);
  const token = issueDemoToken(expiresAt);
  const response = NextResponse.redirect(new URL(returnTo, request.url));

  setAuthCookies(response, request, {
    accessToken: token,
    expiresAt,
    login: demoLogin,
    name: demoName,
  });

  return response;
}

function issueDemoToken(expiresAt: Date): string {
  const issuedAtSeconds = Math.floor(Date.now() / 1000);
  const expiresAtSeconds = Math.floor(expiresAt.getTime() / 1000);
  const header = encodeTokenPart({ alg: "HS256", typ: "JWT" });
  const payload = encodeTokenPart({
    sub: demoUserId,
    iss: process.env.DYNAMIC_PDB_AUTH_JWT_ISSUER ?? "dynamic-pdb-backend",
    iat: issuedAtSeconds,
    exp: expiresAtSeconds,
  });
  const signingInput = `${header}.${payload}`;
  const signature = createHmac(
    "sha256",
    process.env.DYNAMIC_PDB_AUTH_JWT_SECRET ?? "sample-secret",
  )
    .update(signingInput)
    .digest("base64url");

  return `${signingInput}.${signature}`;
}

function encodeTokenPart(value: unknown): string {
  return Buffer.from(JSON.stringify(value)).toString("base64url");
}
