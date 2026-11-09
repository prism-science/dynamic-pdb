import { NextResponse } from "next/server";

import { ApiRequestError, searchFailureFromError, searchModels } from "@/lib/api/entries";
import { readModelSearch } from "@/lib/model-search";

/**
 * Pages a model search for the scroll on /browse: the page renders the first
 * page itself, with the total, and this route the ones after it.
 *
 * It takes the page's own filter parameters plus `cursor` and forwards them
 * unchanged, so later pages answer the same search as the first. A failure
 * comes back with its status and Retry-After seconds, so the list can say what
 * to do instead of stopping as if the results had ended.
 */
export async function GET(request: Request) {
  const url = new URL(request.url);
  const criteria = readModelSearch(url.searchParams);
  const cursor = url.searchParams.get("cursor") || null;

  try {
    const page = await searchModels(criteria, { cursor });
    return NextResponse.json({ items: page.items, nextCursor: page.nextCursor });
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return NextResponse.json(
        { error: searchFailureFromError(error) },
        { status: error.status ?? 502 },
      );
    }
    throw error;
  }
}
