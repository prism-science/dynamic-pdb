require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  MODEL_COLORS,
  MODEL_COLOR_REST,
  comparableModels,
  modelColor,
  modelOverlays,
} = require("../src/lib/model-overlays.ts");

test("should pin a colour to the model's place in the entry, not to the selection", () => {
  // given -- the same entry seen through two different models
  const data = entry([
    modelWith("m1", "Deposited model", "dep.cif", "mmcif"),
    modelWith("m2", "Rerefined model", "reref.pdb", "pdb"),
    modelWith("m3", "qFit model", "qfit.cif", "mmcif"),
  ]);

  // when
  const fromFirst = modelOverlays(data, "m1");
  const fromLast = modelOverlays(data, "m3");

  // then -- m2 is the same colour whichever model the reader is standing on
  const m2From1 = fromFirst.others.find((m) => m.modelId === "m2");
  const m2From3 = fromLast.others.find((m) => m.modelId === "m2");
  assert.equal(m2From1.color, MODEL_COLORS[1]);
  assert.equal(m2From3.color, MODEL_COLORS[1]);
  assert.equal(fromFirst.baseColor, MODEL_COLORS[0]);
  assert.equal(fromLast.baseColor, MODEL_COLORS[2]);
});

test("should name rather than colour a fourth model", () => {
  assert.equal(modelColor(0), MODEL_COLORS[0]);
  assert.equal(modelColor(2), MODEL_COLORS[2]);
  assert.equal(modelColor(3), MODEL_COLOR_REST);
  assert.equal(modelColor(9), MODEL_COLOR_REST);
});

test("should leave the model in view out of its own overlays", () => {
  // given
  const data = entry([
    modelWith("m1", "Deposited model", "dep.cif", "mmcif"),
    modelWith("m2", "qFit model", "qfit.cif", "mmcif"),
  ]);

  // when
  const { others } = modelOverlays(data, "m2");

  // then
  assert.deepEqual(
    others.map((m) => m.modelId),
    ["m1"],
  );
});

test("should offer nothing to overlay when the entry has a single model", () => {
  // given
  const data = entry([modelWith("m1", "Deposited model", "dep.cif", "mmcif")]);

  // then
  const { others, skipped } = modelOverlays(data, "m1");
  assert.deepEqual(others, []);
  assert.equal(skipped, 0);
});

test("should skip a model whose file the viewer has no atoms to superpose from", () => {
  // given -- a model recorded as a density map, and one with no file at all
  const data = entry([
    modelWith("m1", "qFit model", "qfit.cif", "mmcif"),
    modelWith("m2", "Density", "map.ccp4", "ccp4"),
    { id: "m3", title: "Pending model", entity: null },
  ]);

  // when
  const all = comparableModels(data);
  const { others } = modelOverlays(data, "m1");

  // then -- both keep a row, neither is offered for overlay
  assert.deepEqual(
    all.map((m) => m.modelId),
    ["m1", "m2", "m3"],
  );
  assert.equal(all[1].url, null);
  assert.equal(all[2].url, null);
  assert.deepEqual(others, []);
  // counted, so the bar can say why it is empty
  assert.equal(modelOverlays(data, "m1").skipped, 2);
});

test("should read the format from the payload before falling back to the url", () => {
  // given -- a file whose name says nothing and whose payload does
  const data = entry([
    modelWith("m1", "Deposited model", "dep.cif", "mmcif"),
    {
      id: "m2",
      title: "qFit model",
      entity: {
        id: "e-m2",
        type: "model",
        model_id: "m2",
        payload: { file_url: "https://files.example/opaque", type: "pdb" },
      },
    },
  ]);

  // when
  const { others } = modelOverlays(data, "m1");

  // then
  assert.equal(others[0].kind, "pdb");
  assert.equal(others[0].url, "https://files.example/opaque");
});

function entry(models) {
  return {
    entry: { id: "entry-1" },
    models: models.map((m) => ({
      id: m.id,
      entry_id: "entry-1",
      title: m.title,
      created_at: "2026-08-09T00:00:00Z",
      thumbnail_image_url: null,
    })),
    entities: models.map((m) => m.entity).filter(Boolean),
    relations: [],
  };
}

function modelWith(id, title, fileName, type) {
  return {
    id,
    title,
    entity: {
      id: `e-${id}`,
      entry_id: "entry-1",
      model_id: id,
      type: "model",
      level: "L2",
      name: fileName,
      payload: { file_url: `https://files.example/${fileName}`, type },
    },
  };
}
