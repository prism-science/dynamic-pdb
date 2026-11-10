require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

const {
  NOT_FLAGGED,
  canSeeReviewFlags,
  reviewFlagLines,
  reviewStatus,
} = require("../src/lib/review-flags.ts");
const {
  default: ReviewFlagNotice,
  REVIEW_FLAG_TOOLTIP,
} = require("../src/app/components/ReviewFlagNotice.tsx");

function flag(issue, objectType, objectId) {
  return { issue, object_type: objectType, object_id: objectId, flagged_at: "2026-10-23T09:00:00Z" };
}

function render(status) {
  return renderToStaticMarkup(React.createElement(ReviewFlagNotice, { status }));
}

test("a model flagged for review shows only the generic badge when the response names no issue", () => {
  // given: what an anonymous or signed-in caller gets.
  const status = reviewStatus({ flagged_for_review: true });

  // when
  const html = render(status);

  // then
  assert.deepEqual(status, { flagged: true, issueLines: null });
  assert.match(html, /Flagged for review/);
  assert.ok(html.includes(REVIEW_FLAG_TOOLTIP));
  assert.doesNotMatch(html, /<details/);
  assert.doesNotMatch(html, /Which issues/);
});

test("a reviewer can open the badge to read the named issues", () => {
  // given: what a reviewer or admin gets.
  const status = reviewStatus({
    flagged_for_review: true,
    review_flags: [
      flag("mutations_need_review", "polymer_entity", "7"),
      flag("mutations_need_review", "polymer_entity", "8"),
      flag("sync_validation_failed", "entry", "dpdb_abcd1234"),
    ],
  });

  // when
  const html = render(status);

  // then
  assert.deepEqual(status.issueLines, [
    "Mutations need review (2 polymer entities)",
    "The latest PDB update failed validation (entry)",
  ]);
  assert.match(html, /<details/);
  assert.match(html, /Which issues/);
  assert.match(html, /Mutations need review \(2 polymer entities\)/);
});

test("a model that is not flagged shows nothing, whoever asks", () => {
  // given
  const unflagged = reviewStatus({ flagged_for_review: false, review_flags: [] });
  const olderServer = reviewStatus({});

  // when / then
  assert.deepEqual(unflagged, { flagged: false, issueLines: [] });
  assert.deepEqual(olderServer, NOT_FLAGGED);
  assert.equal(render(unflagged), "");
  assert.equal(render(olderServer), "");
});

test("an issue or record type the website does not know is shown by its code", () => {
  // given
  const flags = [flag("metric_key_conflict", "run", "r1")];

  // when
  const lines = reviewFlagLines(flags);

  // then
  assert.deepEqual(lines, ["metric_key_conflict (run)"]);
});

test("only users granted review_flags.read are offered the flagged filter", () => {
  // given / when / then
  assert.equal(canSeeReviewFlags(["revisions.approve", "review_flags.read"]), true);
  assert.equal(canSeeReviewFlags(["revisions.approve", "revisions.reject"]), false);
  assert.equal(canSeeReviewFlags([]), false);
});
