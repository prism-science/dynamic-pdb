import { ApiRequestError, listReviews } from "@/lib/api/entries";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import { isConfiguredAdmin } from "@/app/reviews/admin";

import HeaderBar, { type HeaderReviews, type HeaderUser } from "./HeaderBar";

export default async function AppHeader() {
  const session = await getAuthSession();

  if (!session) {
    return <HeaderBar user={null} reviews={null} />;
  }

  const handle = session.login ?? null;
  const user: HeaderUser = {
    displayName: session.name || (handle ? `@${handle}` : "Account"),
    subLabel: session.name && handle ? `@${handle}` : null,
    initial: (session.name || handle || "?").charAt(0).toUpperCase(),
  };

  const userId = await getCurrentUserId();
  const isReviewer = isConfiguredAdmin(userId);
  const queue = isReviewer
    ? await loadReviewCount(session.token)
    : { toReviewCount: 0, toReviewCountCapped: false };

  const reviews: HeaderReviews = { ...queue, isReviewer };
  return <HeaderBar user={user} reviews={reviews} />;
}

/** The queue has no total to ask for, so the badge counts one capped page and
 *  admits when there are more rather than pretending the cap is the total. */
const BADGE_CAP = 99;

async function loadReviewCount(
  token: string,
): Promise<{ toReviewCount: number; toReviewCountCapped: boolean }> {
  try {
    const page = await listReviews(token, { limit: BADGE_CAP });
    return {
      toReviewCount: page.items.length,
      toReviewCountCapped: page.hasMore,
    };
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return { toReviewCount: 0, toReviewCountCapped: false };
    }
    throw error;
  }
}
