import "server-only";

import { unstable_cache } from "next/cache";

import { ApiRequestError, listReviews } from "@/lib/api/entries";

/** Whether anything is waiting for review — not how much. The queue endpoint has
 *  no total, so a number in the badge could only come from fetching rows, and
 *  fetching a different number of them than the queue itself pages by. One row
 *  is enough to answer the only question the nav has to answer. */
const CACHE_SECONDS = 30;

export const REVIEW_COUNT_TAG = "review-count";

export function hasReviewWork(token: string, userId: string): Promise<boolean> {
  // The token is closed over rather than passed as an argument: unstable_cache
  // keys on its arguments, and a rotating token would never hit the cache.
  const read = unstable_cache(() => fetchHasWork(token), [REVIEW_COUNT_TAG, userId], {
    revalidate: CACHE_SECONDS,
    tags: [REVIEW_COUNT_TAG],
  });
  return read();
}

async function fetchHasWork(token: string): Promise<boolean> {
  try {
    const page = await listReviews(token, { limit: 1 });
    return page.items.length > 0;
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return false;
    }
    throw error;
  }
}
