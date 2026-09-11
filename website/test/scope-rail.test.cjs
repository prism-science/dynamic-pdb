require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { scopeRailModels } = require("../src/lib/scope-rail.ts");
const {
  bestMetricValues,
  metricSpec,
  metricSpecs,
  sortModelsByMetric,
} = require("../src/lib/model-metrics.ts");

test("should_carry_each_models_metrics_into_its_rail_row", () => {
  // given -- two models, one of them with no metrics deposited at all
  const data = entryPage({
    models: [model("m1"), model("m2")],
    entities: [
      modelEntity("e-m1", "m1"),
      modelEntity("e-m2", "m2"),
      metricsEntity("e-metrics", { r_work: 0.184, r_free: 0.226 }),
    ],
    relations: [metricsFor("e-metrics", "e-m1")],
  });

  // when
  const rows = scopeRailModels(data);

  // then
  assert.deepEqual(rows[0].metrics, { r_work: 0.184, r_free: 0.226 });
  assert.deepEqual(rows[1].metrics, {});
});

test("should_name_the_program_that_produced_a_model_with_its_version", () => {
  // given
  const data = entryPage({
    models: [model("m1")],
    entities: [
      modelEntity("e-m1", "m1"),
      programEntity("e-prog", { name: "PDB-REDO", version: "8.03" }),
    ],
    relations: [outputOf("e-m1", "e-prog")],
  });

  // when
  const rows = scopeRailModels(data);

  // then
  assert.equal(rows[0].software, "PDB-REDO 8.03");
});

test("should_leave_the_software_line_empty_when_no_program_was_recorded", () => {
  // given -- a deposit whose provenance was never captured
  const data = entryPage({
    models: [model("m1")],
    entities: [modelEntity("e-m1", "m1")],
    relations: [],
  });

  // when
  const rows = scopeRailModels(data);

  // then
  assert.equal(rows[0].software, null);
});

test("should_name_a_program_without_a_version_by_its_name_alone", () => {
  // given
  const data = entryPage({
    models: [model("m1")],
    entities: [
      modelEntity("e-m1", "m1"),
      programEntity("e-prog", { name: "ISOLDE", version: "" }),
    ],
    relations: [outputOf("e-m1", "e-prog")],
  });

  // when
  const rows = scopeRailModels(data);

  // then
  assert.equal(rows[0].software, "ISOLDE");
});

test("should_fall_back_to_the_entry_thumbnail_when_the_model_has_none", () => {
  // given
  const data = entryPage({
    thumbnail: "https://cdn.example/entry.png",
    models: [model("m1"), { ...model("m2"), thumbnail_image_url: "https://cdn.example/m2.png" }],
    entities: [],
    relations: [],
  });

  // when
  const rows = scopeRailModels(data);

  // then
  assert.equal(rows[0].thumbnailImageURL, "https://cdn.example/entry.png");
  assert.equal(rows[1].thumbnailImageURL, "https://cdn.example/m2.png");
});

test("should_sink_models_without_the_metric_to_the_bottom_whichever_way_it_is_sorted", () => {
  // given -- a predicted model, which has no R-factors at all
  const rows = [
    { id: "predicted", metrics: {} },
    { id: "worse", metrics: { r_free: 0.289 } },
    { id: "better", metrics: { r_free: 0.214 } },
  ];
  const spec = metricSpec("r_free");

  // when
  const rising = sortModelsByMetric(rows, spec, true);
  const falling = sortModelsByMetric(rows, spec, false);

  // then
  assert.deepEqual(rising.map((row) => row.id), ["better", "worse", "predicted"]);
  assert.deepEqual(falling.map((row) => row.id), ["worse", "better", "predicted"]);
});

test("should_leave_the_given_order_untouched_when_sorting", () => {
  // given
  const rows = [
    { id: "a", metrics: { r_free: 0.3 } },
    { id: "b", metrics: { r_free: 0.2 } },
  ];

  // when
  sortModelsByMetric(rows, metricSpec("r_free"), true);

  // then
  assert.deepEqual(rows.map((row) => row.id), ["a", "b"]);
});

test("should_call_the_lowest_r_free_and_the_highest_rscc_the_best_of_the_column", () => {
  // given -- R-factors fall towards better, correlation coefficients rise
  const rows = [
    { metrics: { r_free: 0.231, rscc: 0.96 } },
    { metrics: { r_free: 0.214, rscc: 0.972 } },
    { metrics: {} },
  ];

  // when
  const best = bestMetricValues(rows);

  // then
  assert.equal(best.get("r_free"), 0.214);
  assert.equal(best.get("rscc"), 0.972);
  assert.equal(best.has("clashscore"), false);
});

test("should_ignore_a_sort_key_that_is_not_one_of_the_metrics", () => {
  // given -- whatever a hand-edited URL happens to carry

  // then
  assert.equal(metricSpec("resolution"), null);
  assert.equal(metricSpec(""), null);
  assert.equal(metricSpec(null), null);
  assert.equal(metricSpec("r_work").label, "R-work");
});

test("should_know_which_direction_is_better_for_every_metric_it_offers", () => {
  // given -- the rail picks a default direction from this alone

  // then
  assert.deepEqual(
    metricSpecs.map((spec) => [String(spec.key), spec.lowerIsBetter]),
    [
      ["r_work", true],
      ["r_free", true],
      ["rscc", false],
      ["clashscore", true],
      ["molprobity_score", true],
    ],
  );
});

function entryPage({ models, entities, relations, thumbnail = null }) {
  return {
    entry: { id: "entry-1", thumbnail_image_url: thumbnail },
    models,
    entities,
    relations,
  };
}

function model(id) {
  return {
    id,
    entry_id: "entry-1",
    created_by: "user-1",
    title: `Model ${id}`,
    thumbnail_image_url: null,
    created_at: "2024-03-12T00:00:00Z",
    updated_at: "2024-03-12T00:00:00Z",
  };
}

function modelEntity(id, modelId) {
  return { id, entry_id: "entry-1", model_id: modelId, type: "model", payload: {} };
}

function metricsEntity(id, payload) {
  return { id, entry_id: "entry-1", model_id: null, type: "metrics", payload };
}

function programEntity(id, payload) {
  return { id, entry_id: "entry-1", model_id: null, type: "program", payload };
}

function metricsFor(source, target) {
  return {
    source_entity_id: source,
    target_entity_id: target,
    relation_type: "metrics_for",
  };
}

function outputOf(source, target) {
  return {
    source_entity_id: source,
    target_entity_id: target,
    relation_type: "output_of",
  };
}
