require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { metricScales } = require("../src/lib/model-comparison.ts");

test("should place every model on the metric's own fixed scale, not on the entry's spread", () => {
  // given -- three models whose R-free is bunched in the good half
  const data = entry([
    ["m1", "qFit model", { r_free: 0.163 }],
    ["m2", "Ensemble", { r_free: 0.188 }],
    ["m3", "Deposited", { r_free: 0.234 }],
  ]);

  // when
  const scale = metricScales(data, "m1").find((s) => s.key === "r_free");

  // then -- the axis runs 0.40 (poor) to 0.10 (good), so all three sit right of
  // centre and close together; nobody is pinned to 0 or 100 just for being last
  assert.deepEqual(
    scale.points.map((p) => Math.round(p.position)),
    [79, 71, 55],
  );
  assert.deepEqual(
    scale.ticks.map((t) => t.label),
    ["0.40", "0.30", "0.25", "0.10"],
  );
  assert.equal(scale.currentLabel, "0.163");
  assert.equal(scale.currentStatus, "good");
  assert.equal(scale.points[0].best, true);
  assert.equal(scale.points[0].current, true);
});

test("should cut the track at the metric's thresholds, poor end first", () => {
  // given
  const data = entry([["m1", "A", { r_free: 0.28 }]]);

  // when
  const scale = metricScales(data, "m1").find((s) => s.key === "r_free");

  // then -- bad up to 0.30, acceptable to 0.25, good the rest of the way
  assert.deepEqual(
    scale.bands.map((b) => [b.status, Math.round(b.start), Math.round(b.width)]),
    [
      ["bad", 0, 33],
      ["warn", 33, 17],
      ["good", 50, 50],
    ],
  );
  // and the model standing between the first two cuts is called acceptable
  assert.equal(scale.currentStatus, "warn");
  assert.equal(scale.points[0].status, "warn");
});

test("should run a rising figure the same way round: poor left, good right", () => {
  // given
  const data = entry([
    ["m1", "A", { rscc: 0.91 }],
    ["m2", "B", { rscc: 0.95 }],
  ]);

  // when
  const scale = metricScales(data, "m1").find((s) => s.key === "rscc");

  // then -- 0.60 at the left end, 1.00 at the right, both models near the good end
  assert.deepEqual(
    scale.ticks.map((t) => [t.label, Math.round(t.position)]),
    [
      ["0.60", 0],
      ["0.80", 50],
      ["0.90", 75],
      ["1.00", 100],
    ],
  );
  assert.equal(scale.points[0].position, 77.5);
  assert.equal(scale.points[1].position, 87.5);
});

test("should draw a full track for a figure only one model carries", () => {
  // given -- the case a relative track had nothing to say about
  const data = entry([
    ["m1", "qFit model", { clashscore: 5 }],
    ["m2", "Deposited", { r_free: 0.234 }],
  ]);

  // when
  const scale = metricScales(data, "m1").find((s) => s.key === "clashscore");

  // then -- one dot, on the same scale every clashscore is read against
  assert.equal(scale.recorded, 1);
  assert.equal(scale.total, 2);
  assert.equal(scale.currentLabel, "5.0");
  assert.equal(scale.currentStatus, "good");
  assert.equal(Math.round(scale.points[0].position), 83);
  assert.deepEqual(
    scale.ticks.map((t) => t.label),
    ["30", "20", "10", "0"],
  );
  assert.equal(scale.guide, "good below 10, poor at 20 and above");
});

test("should keep a figure the model in view lacks, as another model's fact", () => {
  // given -- the deposit carries a clashscore, the re-refinement R-factors
  const data = entry([
    ["m1", "Rerefined", { r_free: 0.163 }],
    ["m2", "Deposited", { clashscore: 5 }],
  ]);

  // when
  const scales = metricScales(data, "m1");

  // then -- both rows are drawn, and the one this model has no value for says so
  assert.deepEqual(
    scales.map((s) => s.key),
    ["r_free", "clashscore"],
  );
  const clash = scales[1];
  assert.equal(clash.currentLabel, null);
  assert.equal(clash.currentStatus, null);
  assert.equal(clash.points.length, 1);
});

test("should park a value past the end of the scale at that end and say so", () => {
  // given -- a clashscore worse than the axis goes, and an R-work better than it
  const data = entry([
    ["m1", "Predicted", { clashscore: 45, r_work: 0.05 }],
  ]);

  // when
  const scales = metricScales(data, "m1");
  const clash = scales.find((s) => s.key === "clashscore");
  const rWork = scales.find((s) => s.key === "r_work");

  // then -- clamped to the end, flagged, and still reported at its true value
  assert.equal(clash.points[0].position, 0);
  assert.equal(clash.points[0].offScale, "worse");
  assert.equal(clash.currentLabel, "45.0");
  assert.equal(rWork.points[0].position, 100);
  assert.equal(rWork.points[0].offScale, "better");
});

test("should drop a figure no model of the entry carries", () => {
  // given
  const data = entry([["m1", "A", { r_free: 0.2 }]]);

  // when
  const keys = metricScales(data, "m1").map((s) => s.key);

  // then
  assert.deepEqual(keys, ["r_free"]);
});

function entry(models) {
  const entities = [];
  const relations = [];
  for (const [id, , metrics] of models) {
    entities.push({
      id: `e-${id}`,
      entry_id: "entry-1",
      model_id: id,
      type: "model",
      level: "L2",
      name: `${id}.cif`,
      payload: { file_url: `https://files.example/${id}.cif`, type: "mmcif" },
    });
    if (Object.keys(metrics).length > 0) {
      entities.push({
        id: `metrics-${id}`,
        entry_id: "entry-1",
        model_id: id,
        type: "metrics",
        level: "L3",
        name: "Metrics",
        payload: metrics,
      });
      relations.push({
        source_entity_id: `metrics-${id}`,
        target_entity_id: `e-${id}`,
        relation_type: "metrics_for",
      });
    }
  }
  return {
    entry: { id: "entry-1" },
    models: models.map(([id, title]) => ({
      id,
      entry_id: "entry-1",
      title,
      created_at: "2026-08-09T00:00:00Z",
      thumbnail_image_url: null,
    })),
    entities,
    relations,
  };
}
