import { NextResponse } from "next/server";

import {
  ApiRequestError,
  listUserSubmissions,
  type RevisionState,
} from "@/lib/api/entries";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";
import { REVIEW_PAGE_SIZE } from "@/lib/reviewQueue";
import { SUBMISSION_STATES } from "../submissionTabs";

const maxLimit = 100;

/** Pages the signed-in author's own submissions for the left column of
 *  /entries. Scoped to the caller: the user-scoped backend routes refuse
 *  anything else anyway. */
export async function GET(request: Request) {
  const session = await getAuthSession();
  if (!session) {
    return NextResponse.json({ items: [], hasMore: false }, { status: 401 });
  }
  const userId = userIdFromToken(session.token);
  if (!userId) {
    return NextResponse.json({ items: [], hasMore: false }, { status: 401 });
  }

  const url = new URL(request.url);
  const state = url.searchParams.get("state");
  if (!isSubmissionState(state)) {
    return NextResponse.json({ items: [], hasMore: false }, { status: 400 });
  }
  const limit = boundedPositiveInteger(
    url.searchParams.get("limit"),
    REVIEW_PAGE_SIZE,
    maxLimit,
  );
  const offset = positiveInteger(url.searchParams.get("offset"), 0);

  try {
    const page = await listUserSubmissions(session.token, userId, state, {
      limit,
      offset,
    });
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

function isSubmissionState(value: string | null): value is RevisionState {
  return SUBMISSION_STATES.some((state) => state === value);
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
