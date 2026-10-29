require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  canDeleteEntries,
  hasReviewAccess,
  ownRevisionMessage,
  ownRevisionReason,
  reviewPermissionsFor,
} = require("../src/lib/auth/permissions.ts");

test("should derive review capabilities from permissions", () => {
  assert.deepEqual(reviewPermissionsFor(["revisions.approve"]), {
    canApprove: true,
    canReject: false,
    canApproveOwn: false,
  });
  assert.deepEqual(reviewPermissionsFor(["revisions.reject"]), {
    canApprove: false,
    canReject: true,
    canApproveOwn: false,
  });
  assert.deepEqual(reviewPermissionsFor(["roles.assign"]), {
    canApprove: false,
    canReject: false,
    canApproveOwn: false,
  });
  assert.deepEqual(
    reviewPermissionsFor(["revisions.approve", "revisions.approve_own"]),
    { canApprove: true, canReject: false, canApproveOwn: true },
  );
});

test("should grant review access for either review permission", () => {
  assert.equal(hasReviewAccess(["revisions.approve"]), true);
  assert.equal(hasReviewAccess(["revisions.reject"]), true);
  assert.equal(hasReviewAccess([]), false);
});

test("should allow deleting entries only with the entries.delete permission", () => {
  assert.equal(canDeleteEntries(["revisions.approve", "entries.delete"]), true);
  assert.equal(canDeleteEntries(["revisions.approve", "revisions.reject"]), false);
  assert.equal(canDeleteEntries([]), false);
});

test("should hold back approval only on the viewer's own revision", () => {
  const reviewer = reviewPermissionsFor(["revisions.approve", "revisions.reject"]);

  assert.equal(ownRevisionReason(reviewer, "user-1", "user-1"), ownRevisionMessage);
  assert.equal(ownRevisionReason(reviewer, "user-1", "user-2"), null);
  assert.equal(ownRevisionReason(reviewer, null, "user-1"), null);
});

test("should not hold back approval on the viewer's own revision when self-approval is allowed", () => {
  const devReviewer = reviewPermissionsFor([
    "revisions.approve",
    "revisions.reject",
    "revisions.approve_own",
  ]);

  assert.equal(ownRevisionReason(devReviewer, "user-1", "user-1"), null);
});

test("should not hold back approval for a viewer who cannot approve", () => {
  const rejectOnly = reviewPermissionsFor(["revisions.reject"]);

  assert.equal(ownRevisionReason(rejectOnly, "user-1", "user-1"), null);
});
