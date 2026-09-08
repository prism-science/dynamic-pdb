require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  buildModelMetrics,
  defaultModel,
  modelTitle,
} = require("../src/lib/model-metrics.ts");

test("should lead with the deposited model whatever it was named", () => {
  // given -- the two spellings the archive already uses
  const short = [model("m1", "Rerefined model"), model("m2", "Deposited model")];
  const long = [
    model("m1", "Phenix density refinement"),
    model("m2", "Deposited coordinate model"),
  ];

  // then
  assert.equal(defaultModel(short).id, "m2");
  assert.equal(defaultModel(long).id, "m2");
});

test("should fall back to the first model when none is a deposit", () => {
  // given
  const models = [model("m1", "qFit model"), model("m2", "Ensemble refinement")];

  // then
  assert.equal(defaultModel(models).id, "m1");
  assert.equal(defaultModel([]), null);
});

test("should merge the metrics attached to a model through its entity", () => {
  // given
  const entities = [
    { id: "e-model", type: "model", model_id: "m1", payload: {} },
    { id: "e-rwork", type: "metrics", model_id: "m1", payload: { r_work: 0.184 } },
    { id: "e-rfree", type: "metrics", model_id: "m1", payload: { r_free: 0.226 } },
  ];
  const relations = [
    { source_entity_id: "e-rwork", target_entity_id: "e-model", relation_type: "metrics_for" },
    { source_entity_id: "e-rfree", target_entity_id: "e-model", relation_type: "metrics_for" },
  ];

  // when
  const metrics = buildModelMetrics(entities, relations);

  // then
  assert.deepEqual(metrics.get("m1"), { r_work: 0.184, r_free: 0.226 });
});

function model(id, title) {
  return {
    id,
    entry_id: "entry-1",
    created_by: "user-1",
    title,
    thumbnail_image_url: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

test("should name a model by its title, and fall back for one without", () => {
  // when / then
  assert.equal(modelTitle({ title: "  qFit run 3  " }), "qFit run 3");
  assert.equal(modelTitle({ title: "   " }), "Model");
  assert.equal(modelTitle({ title: null }), "Model");
});
