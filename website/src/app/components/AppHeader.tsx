import {
  ApiRequestError,
  listReviews,
  type ReviewQueueItem,
} from "@/lib/api/entries";
import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import { isConfiguredReviewer } from "@/app/reviews/reviewer";

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

  const [userId, all] = await Promise.all([
    getCurrentUserId(),
    loadReviews(session.token),
  ]);
  const isReviewer = isConfiguredReviewer(userId);
  // A reviewer sees the whole queue. (Own submissions are included so a
  // single-user/test setup can still exercise approve/reject.) The queue is
  // already one row per entry.
  const toReviewCount = isReviewer ? all.length : 0;

  const reviews: HeaderReviews = { toReviewCount, isReviewer };
  return <HeaderBar user={user} reviews={reviews} />;
}

async function loadReviews(token: string): Promise<ReviewQueueItem[]> {
  try {
    return await listReviews(token);
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return [];
    }
    throw error;
  }
}
