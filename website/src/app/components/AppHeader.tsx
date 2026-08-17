import {
  ApiRequestError,
  listReviews,
  type ReviewQueueItem,
} from "@/lib/api/entries";
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
  const all = isReviewer ? await loadReviews(session.token) : [];
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
