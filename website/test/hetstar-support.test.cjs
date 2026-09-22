require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  hetstarCanRender,
  modelRole,
} = require("../src/lib/hetstar-support.ts");

test("should hand an all-mmCIF qFit and deposited pair to the heterogeneity viewer", () => {
  // given
  const data = entry([
    model("m1", "Deposited model", "4gs3.cif", "mmcif"),
    model("m2", "qFit model", "4gs3_qFit_010.cif", "mmcif"),
  ]);

  // then
  assert.equal(hetstarCanRender(data), true);
});

test("should refuse the entry when the model it would load is PDB", () => {
  // given -- the deposited model is CIF, but hetstar loads qFit as A
  const data = entry([
    model("m1", "Deposited model", "4gs3.cif", "mmcif"),
    model("m2", "qFit model", "4gs3_qfit.pdb", "pdb"),
  ]);

  // then
  assert.equal(hetstarCanRender(data), false);
});

test("should refuse the entry when the model it would compare against is PDB", () => {
  // given -- A is CIF and loads; B is the deposited model, and pickPair throws
  // on it before anything is drawn
  const data = entry([
    model("m1", "Deposited model", "4gs3.pdb", "pdb"),
    model("m2", "qFit model", "4gs3_qFit_010.cif", "mmcif"),
  ]);

  // then
  assert.equal(hetstarCanRender(data), false);
});

test("should not mind a PDB model the viewer would never load", () => {
  // given -- the ensemble is PDB, but a qFit model exists, so the pair is
  // qFit + deposited and the ensemble is not opened
  const data = entry([
    model("m1", "Deposited model", "4gs3.cif", "mmcif"),
    model("m2", "qFit model", "4gs3_qFit_010.cif", "mmcif"),
    model("m3", "Ensemble", "4gs3_final_ensemble.pdb", "pdb"),
  ]);

  // then
  assert.equal(hetstarCanRender(data), true);
});

test("should take an ensemble as the model to load when there is no qFit", () => {
  // given -- an NMR ensemble on its own, which hetstar loads as A with no B
  assert.equal(
    hetstarCanRender(entry([model("m1", "Ensemble", "ens.cif", "mmcif")])),
    true,
  );
  assert.equal(
    hetstarCanRender(entry([model("m1", "Ensemble", "ens.pdb", "pdb")])),
    false,
  );
});

test("should load a deposited model alone rather than looking for a partner", () => {
  // given -- A is the deposited model itself, so there is no B to fail on
  const data = entry([model("m1", "Deposited model", "4gs3.cif", "mmcif")]);

  // then
  assert.equal(hetstarCanRender(data), true);
});

test("should refuse an entry with no model the viewer recognises", () => {
  // given -- a role it does not load, and a model with no coordinates at all
  assert.equal(
    hetstarCanRender(entry([model("m1", "Rerefined model", "r.cif", "mmcif")])),
    false,
  );
  assert.equal(
    hetstarCanRender({
      entry: { id: "entry-1" },
      models: [{ id: "m1", title: "qFit model" }],
      entities: [],
      relations: [],
    }),
    false,
  );
});

test("should read the format from the catalogue before the file name", () => {
  // given -- a url that says nothing and a payload that says PDB
  const data = entry([
    {
      id: "m1",
      title: "qFit model",
      entities: [
        {
          id: "e-m1",
          type: "model",
          model_id: "m1",
          payload: { file_url: "https://files.example/opaque", type: "pdb" },
        },
      ],
    },
  ]);

  // then
  assert.equal(hetstarCanRender(data), false);
});

test("should prefer the model's primary artifact over the first file listed", () => {
  // given -- two coordinate files on one model, the primary one CIF
  const data = entry([
    {
      id: "m1",
      title: "qFit model",
      primary_artifact_id: "e-cif",
      entities: [
        {
          id: "e-pdb",
          type: "model",
          model_id: "m1",
          payload: { file_url: "https://files.example/a.pdb", type: "pdb" },
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
  assert.equal(hetstarCanRender(data), true);
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
