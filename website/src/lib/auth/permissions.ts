export type ReviewPermissions = {
  canApprove: boolean;
  canReject: boolean;
  /** True where the backend allows self-approval (dev only). */
  canApproveOwn: boolean;
};

const revisionsApprove = "revisions.approve";
const revisionsReject = "revisions.reject";
const revisionsApproveOwn = "revisions.approve_own";
const entriesDelete = "entries.delete";

export function reviewPermissionsFor(
  permissions: readonly string[],
): ReviewPermissions {
  const assigned = new Set(permissions);

  return {
    canApprove: assigned.has(revisionsApprove),
    canReject: assigned.has(revisionsReject),
    canApproveOwn: assigned.has(revisionsApproveOwn),
  };
}

/** What the review card says on a reviewer's own revision. */
export const ownRevisionMessage =
  "You submitted this, so another reviewer must approve it.";

/** Why the viewer may not approve a revision they created, or null when they
 *  may or cannot approve at all. The backend refuses such an approval
 *  (403 SELF_REVIEW) so that publishing a change always takes a second person;
 *  rejecting one's own revision stays allowed. Where the backend allows
 *  self-approval (dev), it grants revisions.approve_own at sign-in and nothing
 *  is held back. */
export function ownRevisionReason(
  reviewPermissions: ReviewPermissions,
  viewerId: string | null,
  createdBy: string,
): string | null {
  if (!reviewPermissions.canApprove || reviewPermissions.canApproveOwn) {
    return null;
  }
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
