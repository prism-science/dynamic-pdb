require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  dataTableEntities,
  modelScopedEntities,
} = require("../src/lib/entities.ts");

test("should keep the model's own artifacts and the entry's, and drop other models'", () => {
  // given -- the API records model_id on model-level files and null on the
  // entry's own
  const entities = [
    entity({ id: "reflections", modelId: null }),
    entity({ id: "sequence", modelId: null }),
    entity({ id: "mine", modelId: "m1" }),
    entity({ id: "metrics-mine", modelId: "m1", type: "metrics", level: "L3" }),
    entity({ id: "theirs", modelId: "m2" }),
    entity({ id: "metrics-theirs", modelId: "m2", type: "metrics", level: "L3" }),
  ];

  // when
  const scoped = modelScopedEntities(entities, "m1");

  // then
  assert.deepEqual(
    scoped.map((item) => item.id),
    ["reflections", "sequence", "mine", "metrics-mine"],
  );
});

test("should keep only the entry's own artifacts when no model is selected", () => {
  // given
  const entities = [
    entity({ id: "reflections", modelId: null }),
    entity({ id: "mine", modelId: "m1" }),
  ];

  // when / then
  assert.deepEqual(
    modelScopedEntities(entities, null).map((item) => item.id),
    ["reflections"],
  );
});

test("should place only levelled, non-program artifacts in the level table", () => {
  // given
  const entities = [
    entity({ id: "raw", level: "L0" }),
    entity({ id: "program", type: "program", level: "L1" }),
    entity({ id: "unlevelled", level: null }),
  ];

  // when / then
  assert.deepEqual(
    dataTableEntities(entities).map((item) => item.id),
    ["raw"],
  );
});

function entity({ id, modelId = null, type = "data", level = "L1" }) {
  return {
    id,
    entry_id: "e1",
    model_id: modelId,
    type,
    level,
    name: id,
    payload: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}
