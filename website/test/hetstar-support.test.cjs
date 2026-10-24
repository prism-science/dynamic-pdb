require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  hetstarCanRender,
  modelRole,
} = require("../src/lib/hetstar-support.ts");

test("should hand the selected qFit model and the deposited model to the heterogeneity viewer", () => {
  // given
  const data = entry([
    model("m1", "Deposited model", "4gs3.cif", "mmcif"),
    model("m2", "qFit model", "4gs3_qFit_010.cif", "mmcif"),
  ]);

  // when
  const canRender = hetstarCanRender(data, "m2");

  // then
  assert.equal(canRender, true);
});

test("should hand a selected PDB model to the viewer, which converts it on load", () => {
  // given -- re-refined and ensemble models are PDB across the catalogue
  const data = entry([
    model("m1", "Deposited model", "4gs3.cif", "mmcif"),
    model("m2", "Rerefined model", "4gs3_020.pdb", "pdb"),
    model("m3", "Ensemble refinement model", "4gs3_final_ensemble.pdb", "pdb"),
  ]);

  // then
  assert.equal(hetstarCanRender(data, "m2"), true);
  assert.equal(hetstarCanRender(data, "m3"), true);
});

test("should load the selected deposited model alone rather than looking for a partner", () => {
  // given -- the qFit model has no coordinates, but it is not the one selected
  const data = entry([
    model("m1", "Deposited model", "4gs3.cif", "mmcif"),
    { id: "m2", title: "qFit model" },
  ]);

  // then
  assert.equal(hetstarCanRender(data, "m1"), true);
});

test("should refuse a selected model when the deposited model it is compared against has no coordinates", () => {
  // given
  const data = entry([
    { id: "m1", title: "Deposited model" },
    model("m2", "qFit model", "4gs3_qFit_010.cif", "mmcif"),
  ]);

  // then
  assert.equal(hetstarCanRender(data, "m2"), false);
});

test("should refuse a selected model with no coordinates, or one the entry does not have", () => {
  // given
  const data = entry([
    model("m1", "Deposited model", "4gs3.cif", "mmcif"),
    { id: "m2", title: "qFit model" },
  ]);

  // then
  assert.equal(hetstarCanRender(data, "m2"), false);
  assert.equal(hetstarCanRender(data, "m9"), false);
});

test("should read the format from the catalogue before the file name", () => {
  // given -- a url that says PDB and a payload that says it is a map
  const data = entry([
    {
      id: "m1",
      title: "Deposited model",
      entities: [
        {
          id: "e-m1",
          type: "model",
          model_id: "m1",
          payload: { file_url: "https://files.example/a.pdb", type: "ccp4" },
        },
      ],
    },
  ]);

  // then
  assert.equal(hetstarCanRender(data, "m1"), false);
});

test("should prefer the model's primary artifact over the first file listed", () => {
  // given -- two files on one model, the primary one CIF
  const data = entry([
    {
      id: "m1",
      title: "Deposited model",
      primary_artifact_id: "e-cif",
      entities: [
        {
          id: "e-map",
          type: "model",
          model_id: "m1",
          payload: { file_url: "https://files.example/a.map", type: "ccp4" },
        },
        {
          id: "e-cif",
          type: "model",
          model_id: "m1",
          payload: { file_url: "https://files.example/a.cif", type: "mmcif" },
        },
      ],
    },
  ]);

  // then
  assert.equal(hetstarCanRender(data, "m1"), true);
});

test("should fall back to the recorded model type when the title does not say", () => {
  assert.equal(modelRole({ title: "qFit model" }), "qfit");
  assert.equal(modelRole({ title: "Re-refined model" }), "rerefined");
  assert.equal(
    modelRole({ title: "Model 3", metadata: { model_type: "deposited" } }),
    "deposited",
  );
  assert.equal(modelRole({ title: "Model 3" }), "other");
  assert.equal(modelRole({ title: null }), "other");
});

function entry(models) {
  return {
    entry: { id: "entry-1" },
    models: models.map((m) => ({
      id: m.id,
      entry_id: "entry-1",
      title: m.title,
      primary_artifact_id: m.primary_artifact_id ?? null,
      created_at: "2026-08-09T00:00:00Z",
      thumbnail_image_url: null,
    })),
    entities: models.flatMap((m) => m.entities ?? []),
    relations: [],
  };
}

function model(id, title, fileName, type) {
  return {
    id,
    title,
    entities: [
      {
        id: `e-${id}`,
        entry_id: "entry-1",
        model_id: id,
        type: "model",
        level: "L2",
        name: fileName,
        payload: { file_url: `https://files.example/${fileName}`, type },
      },
    ],
  };
}
