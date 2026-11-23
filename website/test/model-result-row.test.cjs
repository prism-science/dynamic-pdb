require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

const ModelResultRow = require("../src/app/components/model-search/ModelResultRow.tsx").default;

/** A model search result as GET /v1/models returns it, with the review fields given. */
function result(review) {
  return {
    id: "model-1",
    entry_id: "entry-1",
    title: "Ensemble model",
    metadata: { model_type: "Ensemble", purpose: null },
    metrics: [],
    entry: { id: "entry-1", title: "Kinase", pdb_id: "1ABC", related_datasets: [] },
    ...review,
  };
}

function renderRow(review) {
  return renderToStaticMarkup(React.createElement(ModelResultRow, { result: result(review) }));
}

test("should show a flagged model's badge without naming its issues to the public", () => {
  const html = renderRow({ flagged_for_review: true });

  assert.match(html, /<strong>Flagged for review<\/strong><\/span>/);
  assert.doesNotMatch(html, /Mutations need review/);
});

test("should name a flagged model's issues when the API sent them, as it does to reviewers and admins", () => {
  const html = renderRow({
    flagged_for_review: true,
    review_flags: [
      { object_type: "polymer_entity", object_id: "p1", issue: "mutations_need_review", flagged_at: "2026-11-10T00:00:00Z" },
      { object_type: "polymer_entity", object_id: "p2", issue: "mutations_need_review", flagged_at: "2026-11-10T00:00:00Z" },
    ],
  });

  assert.match(html, /<strong>Flagged for review<\/strong>: Mutations need review \(2 polymer entities\)/);
});

test("should show no badge for a model without flags, or from a server that does not say", () => {
  assert.doesNotMatch(renderRow({ flagged_for_review: false }), /Flagged for review/);
  assert.doesNotMatch(renderRow({}), /Flagged for review/);
});
