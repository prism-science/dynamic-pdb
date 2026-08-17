import "server-only";

/** The backend remains the authority; this only controls admin navigation. */
export function isConfiguredAdmin(userId: string | null): boolean {
  const adminUserId = process.env.ADMIN_USER_ID?.trim();
  return Boolean(adminUserId) && Boolean(userId) && userId === adminUserId;
}
