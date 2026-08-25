import { NextResponse } from "next/server";

import { ApiRequestError, listReviews } from "@/lib/api/entries";
import { hasReviewAccess } from "@/lib/auth/permissions";
import { getAuthSession } from "@/lib/auth/session";
import { REVIEW_PAGE_SIZE } from "@/lib/reviewQueue";

const maxLimit = 100;

/** Pages the review queue for the inbox's left column. The queue is reviewer-only,
 *  so this checks the session rather than proxying it blind. */
export async function GET(request: Request) {
  const session = await getAuthSession();
  if (!session) {
    return NextResponse.json({ items: [] }, { status: 401 });
  }
  if (!hasReviewAccess(session.permissions)) {
    return NextResponse.json({ items: [] }, { status: 403 });
  }

  const url = new URL(request.url);
  const limit = boundedPositiveInteger(
    url.searchParams.get("limit"),
    REVIEW_PAGE_SIZE,
    maxLimit,
  );
  const offset = positiveInteger(url.searchParams.get("offset"), 0);

  try {
    const page = await listReviews(session.token, { limit, offset });
    return NextResponse.json(page);
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return NextResponse.json(
        { items: [], hasMore: false },
        { status: error.status ?? 502 },
      );
    }
    throw error;
  }
}

function boundedPositiveInteger(
  value: string | null,
  fallback: number,
  max: number,
) {
  return Math.min(positiveInteger(value, fallback), max);
}

function positiveInteger(value: string | null, fallback: number) {
  if (value == null) {
    return fallback;
  }
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
