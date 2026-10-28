require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  canDeleteEntries,
  hasReviewAccess,
  reviewPermissionsFor,
} = require("../src/lib/auth/permissions.ts");

test("should derive review capabilities from permissions", () => {
  assert.deepEqual(reviewPermissionsFor(["revisions.approve"]), {
    canApprove: true,
    canReject: false,
  });
  assert.deepEqual(reviewPermissionsFor(["revisions.reject"]), {
    canApprove: false,
    canReject: true,
  });
  assert.deepEqual(reviewPermissionsFor(["roles.assign"]), {
    canApprove: false,
    canReject: false,
  });
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
