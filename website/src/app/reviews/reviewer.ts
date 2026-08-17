import "server-only";

/**
 * Whether the signed-in user is the configured reviewer. This mirrors the
 * backend's hardcoded reviewer (auth.reviewer_user_id): set REVIEWER_USER_ID in
 * the website environment to the same users.id. It only gates the UI — the
 * backend still enforces the decision permission.
 */
export function isConfiguredReviewer(userId: string | null): boolean {
  const reviewerId = process.env.REVIEWER_USER_ID?.trim();
  return Boolean(reviewerId) && Boolean(userId) && userId === reviewerId;
}
