import { NextResponse } from "next/server";

import { ApiRequestError, listSimilarEntries } from "@/lib/api/entries";

/* Page size for the dialog's infinite scroll; the cap mirrors the entries
   feed so one request stays a bounded amount of work for the backend. */
const defaultLimit = 50;
const maxLimit = 100;

type SimilarFeedRouteParams = {
  params: Promise<{ entryId: string }>;
};

/**
 * Feed for the "All similar" dialog, one page of similar entries at a time —
 * the same shape as the entries feed. Like that feed it queries without a
 * token: the similarity listing is public registry data.
 */
export async function GET(
  request: Request,
  { params }: SimilarFeedRouteParams,
) {
  const { entryId } = await params;
  const url = new URL(request.url);
  const limit = boundedPositiveInteger(
    url.searchParams.get("limit"),
    defaultLimit,
    maxLimit,
  );
  const offset = positiveInteger(url.searchParams.get("offset"), 0);

  try {
    const items = await listSimilarEntries(undefined, entryId, {
      limit,
      offset,
    });
    return NextResponse.json({ items });
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return NextResponse.json({ items: [] }, { status: error.status ?? 502 });
    }
    throw error;
  }
}

function boundedPositiveInteger(
  value: string | null,
  fallback: number,
  max: number,
) {
  const parsed = positiveInteger(value, fallback);
  return Math.min(parsed, max);
}

function positiveInteger(value: string | null, fallback: number) {
  if (value == null) {
    return fallback;
  }
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
