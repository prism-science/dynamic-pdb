import { getAuthSession, getCurrentUserId } from "@/lib/auth/session";
import { isConfiguredAdmin } from "@/app/reviews/admin";
import { hasReviewWork } from "@/app/reviews/count";

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
  const hasWork =
    isReviewer && userId ? await hasReviewWork(session.token, userId) : false;

  const reviews: HeaderReviews = { isReviewer, hasWork };
  return <HeaderBar user={user} reviews={reviews} />;
}
