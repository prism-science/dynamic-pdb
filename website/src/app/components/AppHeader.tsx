import { hasReviewWork } from "@/app/reviews/count";
import { hasReviewAccess } from "@/lib/auth/permissions";
import { getAuthSession, userIdFromToken } from "@/lib/auth/session";

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

  const userId = userIdFromToken(session.token);
  const isReviewer = hasReviewAccess(session.permissions);
  const hasWork =
    isReviewer && userId ? await hasReviewWork(session.token, userId) : false;

  const reviews: HeaderReviews = { isReviewer, hasWork };
  return <HeaderBar user={user} reviews={reviews} />;
}
