export type ReviewPermissions = {
  canApprove: boolean;
  canReject: boolean;
};

const revisionsApprove = "revisions.approve";
const revisionsReject = "revisions.reject";
const entriesDelete = "entries.delete";

export function reviewPermissionsFor(
  permissions: readonly string[],
): ReviewPermissions {
  const assigned = new Set(permissions);

  return {
    canApprove: assigned.has(revisionsApprove),
    canReject: assigned.has(revisionsReject),
  };
}

export function hasReviewAccess(permissions: readonly string[]): boolean {
  const reviewPermissions = reviewPermissionsFor(permissions);
  return reviewPermissions.canApprove || reviewPermissions.canReject;
}

/** Whether the session may delete entries. The backend grants entries.delete
 *  at sign-in only to users who can approve revisions, and only where entry
 *  deletion is turned on (dev). */
export function canDeleteEntries(permissions: readonly string[]): boolean {
  return permissions.includes(entriesDelete);
}
