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

/** What the review card says on a reviewer's own revision. */
export const ownRevisionMessage =
  "You submitted this, so another reviewer must approve it.";

/** Why the viewer may not approve a revision they created, or null when they
 *  may. The backend refuses such an approval (403 SELF_REVIEW) so that
 *  publishing a change always takes a second person; rejecting one's own
 *  revision stays allowed. */
export function ownRevisionReason(
  viewerId: string | null,
  createdBy: string,
): string | null {
  return viewerId !== null && viewerId === createdBy ? ownRevisionMessage : null;
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
