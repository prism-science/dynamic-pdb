require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  buildModelMetrics,
  csvFileName,
  defaultModel,
  metricsCSV,
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

test("should write a metrics record as two columns, known figures first", () => {
  // given: the record's own order is not the order the dialog shows
  const payload = {
    rscc: 0.941,
    molprobity_score: 1.42,
    clashscore: 4.2,
    r_free: 0.2255,
    r_work: 0.1839,
  };

  // when / then
  assert.equal(
    metricsCSV(payload),
    [
      "Metric,Value",
      "R-work,0.1839",
      "R-free,0.2255",
      "Clashscore,4.2",
      "MolProbity score,1.42",
      "RSCC,0.941",
    ].join("\r\n"),
  );
});

test("should carry figures the dialog has no name for rather than drop them", () => {
  // given
  const payload = { r_free: 0.2255, custom_score: 4.2, refined_by: "PHENIX" };

  // when
  const lines = metricsCSV(payload).split("\r\n");

  // then: the known one under its name, the rest under their own keys
  assert.deepEqual(lines, [
    "Metric,Value",
    "R-free,0.2255",
    "custom_score,4.2",
    "refined_by,PHENIX",
  ]);
});

test("should omit unsupported CC metric", () => {
  // when / then
  assert.equal(
    metricsCSV({ r_free: 0.2255, cc: 0.98 }),
    ["Metric,Value", "R-free,0.2255"].join("\r\n"),
  );
});

test("should write values as stored, not as the screen rounds them", () => {
  // given: three decimals on screen, and a thousands separator would be a
  // column break
  const payload = { r_free: 0.22551234, reflections: 36853 };

  // when / then
  assert.equal(
    metricsCSV(payload),
    ["Metric,Value", "R-free,0.22551234", "reflections,36853"].join("\r\n"),
  );
});

test("should quote a field that would otherwise be read as structure", () => {
  // given
  const payload = { note: 'refined, then "rebuilt"', trailing: " padded " };

  // when
  const lines = metricsCSV(payload).split("\r\n");

  // then: the trailing value is trimmed, so it needs no quoting
  assert.deepEqual(lines, [
    "Metric,Value",
    'note,"refined, then ""rebuilt"""',
    "trailing,padded",
  ]);
});

test("should skip what is not a figure at all", () => {
  // when / then
  assert.equal(metricsCSV({}), "Metric,Value");
  assert.equal(metricsCSV({ r_free: null, rscc: NaN }), "Metric,Value");
});

test("should name the file after the record, ending in .csv once", () => {
  // when / then
  assert.equal(csvFileName("qfit_run3_metrics"), "qfit_run3_metrics.csv");
  assert.equal(csvFileName("metrics.csv"), "metrics.csv");
  assert.equal(csvFileName("run 3/final:metrics"), "run 3-final-metrics.csv");
  assert.equal(csvFileName("   "), "metrics.csv");
});
