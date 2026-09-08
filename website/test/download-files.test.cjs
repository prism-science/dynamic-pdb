require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { downloadGroups } = require("../src/lib/download-files.ts");

test("should offer the sequence, the model's coordinates and its structure factors, in that order", () => {
  // given
  const entities = [
    artifact({ id: "fasta", modelId: null, type: "data", format: "fasta" }),
    artifact({ id: "coords", modelId: "m1", type: "model", format: "mmcif" }),
    artifact({
      id: "sf",
      modelId: "m1",
      type: "data",
      format: "structure-factor-cif",
    }),
  ];

  // when
  const groups = downloadGroups(entities, "m1");

  // then
  assert.deepEqual(
    groups.map((group) => group.files.map((file) => file.label)),
    [
      ["FASTA Sequence"],
      ["Coordinates (mmCIF)"],
      ["Structure Factors (CIF)"],
    ],
  );
});

test("should offer the selected model's files and not another model's", () => {
  // given
  const entities = [
    artifact({ id: "mine", modelId: "m1", type: "model", format: "mmcif" }),
    artifact({ id: "theirs", modelId: "m2", type: "model", format: "mmcif" }),
  ];

  // when
  const groups = downloadGroups(entities, "m1");

  // then
  assert.deepEqual(
    groups.flatMap((group) => group.files.map((file) => file.id)),
    ["mine"],
  );
});

test("should leave out a file that has no address, rather than offering a dead link", () => {
  // given
  const entities = [
    artifact({ id: "fasta", modelId: null, type: "data", format: "fasta" }),
    {
      ...artifact({ id: "coords", modelId: "m1", type: "model", format: "mmcif" }),
      payload: { type: "mmcif" },
    },
  ];

  // when
  const groups = downloadGroups(entities, "m1");

  // then
  assert.deepEqual(
    groups.map((group) => group.key),
    ["sequence"],
  );
});

test("should recognise structure factors however the cif is named", () => {
  // given
  const formats = ["structure-factor-cif", "sf-cif", "SF_CIF", "sf.cif"];

  // when
  const keys = formats.map(
    (format) =>
      downloadGroups(
        [artifact({ id: format, modelId: null, type: "data", format })],
        null,
      )[0]?.key,
  );

  // then
  assert.deepEqual(keys, ["factors", "factors", "factors", "factors"]);
});

test("should not offer reflections as structure factors: we only publish the cif", () => {
  // given
  const entities = [
    artifact({ id: "mtz", modelId: "m1", type: "data", format: "mtz" }),
    artifact({ id: "map", modelId: "m1", type: "data", format: "ccp4" }),
  ];

  // when / then
  assert.deepEqual(downloadGroups(entities, "m1"), []);
});

test("should offer nothing when the entry has none of the named files", () => {
  // given
  const entities = [
    artifact({ id: "frames", modelId: null, type: "data", format: "hdf5" }),
  ];

  // when / then
  assert.deepEqual(downloadGroups(entities, null), []);
});

function artifact({ id, modelId, type, format }) {
  return {
    id,
    entry_id: "e1",
    model_id: modelId,
    type,
    name: id,
    level: "L1",
    payload: { type: format, file_url: `https://files.example/${id}` },
  };
}
