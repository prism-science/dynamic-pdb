import { randomInt } from "node:crypto";

import { request } from "@playwright/test";

import { expectStatus } from "./api";
import { stack } from "./stack";

// Controls the fake RCSB (e2e/stack/fake-rcsb.mjs) for one PDB ID.

const PDB_ID_CHARACTERS = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";

/**
 * A PDB ID no other test uses, "9" and three random characters. Each test needs
 * its own: the backend caches a successful lookup for 10 minutes, and only one
 * active entry may name a PDB ID.
 */
export function freshPdbId(): string {
  let id = "9";
  for (let index = 0; index < 3; index++) {
    id += PDB_ID_CHARACTERS[randomInt(PDB_ID_CHARACTERS.length)];
  }
  return id;
}

/** The title the fake archive gives every entry (e2e/fixtures/rcsb-archive.json). */
export function archiveTitle(pdbId: string): string {
  return `Archive structure of ${pdbId}`;
}

/** The archive version the fake reports, as the source badges show it. */
export const ARCHIVE_VERSION = "1.1";

async function call(method: "PUT" | "POST" | "DELETE", path: string, body?: unknown) {
  const context = await request.newContext({ baseURL: stack.rcsbURL });
  try {
    const response = await context.fetch(path, { method, data: body });
    if (response.status() !== 204) {
      await expectStatus(response, 204, `fake RCSB ${method} ${path}`);
    }
  } finally {
    await context.dispose();
  }
}

/** Merges fields over the archive entry the fake returns for pdbId. */
export function setArchiveEntryFields(pdbId: string, fields: Record<string, unknown>) {
  return call("PUT", `/__fake/entries/${pdbId}`, fields);
}

/**
 * Makes every RCSB request for pdbId fail. The default 403 is not retried by
 * the backend, which retries 5xx answers for about 10 seconds first.
 */
export function failArchiveEntry(pdbId: string, status = 403) {
  return call("POST", `/__fake/entries/${pdbId}/fail?status=${status}`);
}

/** Answers requests for pdbId normally again. */
export function restoreArchiveEntry(pdbId: string) {
  return call("DELETE", `/__fake/entries/${pdbId}/fail`);
}
