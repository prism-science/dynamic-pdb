require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { ApiRequestError } = require("../src/lib/api/entries.ts");
const { decisionErrorMessage } = require("../src/app/reviews/decisionMessage.ts");
const { ownRevisionMessage } = require("../src/lib/auth/permissions.ts");

const groupingDetail =
  "the model's PDB ID is 44PS and the entry's PDB ID is 7KR0: a PDB deposition is filed under another entry " +
  "only once the grouping rules (same experimental data) pass, so it cannot be approved yet; reject it or leave it in review";

test("should show why a filing under another entry cannot be approved, in the API's words", () => {
  const error = new ApiRequestError("conflict", 409, [{ code: "GROUPING_NOT_APPROVED", detail: groupingDetail }]);

  assert.equal(decisionErrorMessage(error, "Failed to approve."), groupingDetail);
});

test("should keep the other decision messages", () => {
  assert.equal(
    decisionErrorMessage(new ApiRequestError("conflict", 409, [{ code: "CONFLICT" }]), "Failed to approve."),
    "This submission changed while you were looking at it.",
  );
  assert.equal(
    decisionErrorMessage(new ApiRequestError("forbidden", 403, [{ code: "SELF_REVIEW" }]), "Failed to approve."),
    ownRevisionMessage,
  );
  assert.equal(
    decisionErrorMessage(new ApiRequestError("forbidden", 403, [{ code: "FORBIDDEN" }]), "Failed to approve."),
    "You don't have permission to make this decision.",
  );
  assert.equal(
    decisionErrorMessage(new ApiRequestError("gone", 404, []), "Failed to approve."),
    "This submission is no longer in the queue.",
  );
  assert.equal(decisionErrorMessage("nope", "Failed to approve."), "Failed to approve.");
});
