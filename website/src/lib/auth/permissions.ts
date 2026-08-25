export type ReviewPermissions = {
  canApprove: boolean;
  canReject: boolean;
};

const revisionsApprove = "revisions.approve";
const revisionsReject = "revisions.reject";

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
