import "server-only";

const localAdminUserIds: string[] = [];
const productionAdminUserIds: string[] = [
  "5b8670ac-41c9-4e1d-a436-da6daa2a8c22",
];

// TODO: Replace this temporary frontend-only allowlist with an authenticated
// backend capability check. The backend must remain the source of truth for
// administrator permissions; this file should only decide what navigation to
// show once the API exposes the current user's admin/reviewer capabilities.
/** The backend remains the authority; this only controls admin navigation. */
export function isConfiguredAdmin(userId: string | null): boolean {
  if (!userId) {
    return false;
  }

  const adminUserIds = hardcodedAdminUserIds();

  return adminUserIds.includes(userId);
}

function hardcodedAdminUserIds(): string[] {
  if (process.env.APP_BASE_URL?.trim() === "https://dynamicpdb.com") {
    return productionAdminUserIds;
  }
  return localAdminUserIds;
}
