import { createHmac } from "node:crypto";

import type { BrowserContext } from "@playwright/test";

import { stack } from "./stack";

export type TestUser = {
  id: string;
  login: string;
  name: string;
  /** Only changes what the website shows; the backend reads permissions from its database. */
  permissions: string[];
};

/** A depositor with no role, as created by e2e/stack/seed.sql. */
export const submitter: TestUser = {
  id: stack.submitterId,
  login: "browser-test-submitter",
  name: "Submitter",
  permissions: [],
};

/** A reviewer, as created by e2e/stack/seed.sql. It approves the submitter's revisions. */
export const reviewer: TestUser = {
  id: stack.reviewerId,
  login: "browser-test-reviewer",
  name: "Reviewer",
  permissions: ["revisions.approve", "revisions.reject"],
};

/** A token like the one backend/internal/auth/jwt.go issues: HS256 with sub, iss, iat and exp. */
export function mintToken(user: TestUser): string {
  const issuedAt = Math.floor(Date.now() / 1000);
  const header = base64url({ alg: "HS256", typ: "JWT" });
  const claims = base64url({
    sub: user.id,
    iss: stack.jwtIssuer,
    iat: issuedAt,
    exp: issuedAt + 60 * 60,
  });
  const signature = createHmac("sha256", stack.jwtSecret)
    .update(`${header}.${claims}`)
    .digest("base64url");
  return `${header}.${claims}.${signature}`;
}

/**
 * Signs the browser in as user with the cookies that the GitHub login sets
 * (website/src/lib/auth/session.ts, setAuthCookies), so no OAuth runs.
 */
export async function signIn(context: BrowserContext, user: TestUser): Promise<void> {
  const cookie = (name: string, value: string) => ({
    name,
    value,
    url: stack.websiteURL,
    httpOnly: true,
    sameSite: "Lax" as const,
  });
  await context.addCookies([
    cookie("dynamic_pdb_auth_token", mintToken(user)),
    cookie("dynamic_pdb_auth_login", user.login),
    cookie("dynamic_pdb_auth_name", user.name),
    cookie("dynamic_pdb_auth_permissions", base64url(user.permissions)),
  ]);
}

function base64url(value: unknown): string {
  return Buffer.from(JSON.stringify(value), "utf8").toString("base64url");
}
