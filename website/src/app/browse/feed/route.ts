import { NextResponse } from "next/server";

import { ApiRequestError, listEntriesPage } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";

const defaultLimit = 50;
const maxLimit = 100;

/**
 * Pages the public entries list for the infinite scroll on /browse.
 *
 * It exists because that scroll used to call /entries/feed, which is a
 * different feature: the signed-in author's own submissions, keyed by a
 * mandatory `state` parameter. Without that parameter it answered 400 — 401 for
 * an anonymous visitor — the scroller read the failure as "no more entries" and
 * quietly stopped after the first server-rendered page. Keeping this route next
 * to the page it feeds is what stops the two from drifting apart again.
 *
 * Public, like the page: an absent or stale session just means the request goes
 * to the backend unauthenticated.
 *
 * Pages by the backend's opaque `cursor`; the response's `nextCursor` is null on
 * the last page.
 */
export async function GET(request: Request) {
  const url = new URL(request.url);
  const query = url.searchParams.get("query")?.trim() || undefined;
  const limit = Math.min(
    positiveInteger(url.searchParams.get("limit"), defaultLimit),
    maxLimit,
  );
  const cursor = url.searchParams.get("cursor") || undefined;

  const session = await getAuthSession();

  try {
    const page = await listEntriesPage(session?.token, { query, limit, cursor });
    return NextResponse.json({ items: page.items, nextCursor: page.nextCursor });
  } catch (error) {
    // An empty last page reads as the end of the list, which is the right thing
    // for the scroller to do with an error it cannot act on.
    if (error instanceof ApiRequestError) {
      return NextResponse.json(
        { items: [], nextCursor: null },
        { status: error.status ?? 502 },
      );
    }
    throw error;
  }
}

function positiveInteger(value: string | null, fallback: number) {
  if (value == null) {
    return fallback;
  }
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
