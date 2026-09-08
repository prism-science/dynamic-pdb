require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  crystallographyView,
  formatKelvin,
  formatPH,
} = require("../src/lib/crystallography.ts");

test("should give every dataset its own row and repeat the crystal's conditions", () => {
  // given -- 1FFK: one crystal, two datasets
  const crystallography = {
    crystals: [
      {
        id: "1",
        growth: { ph: 5.8, temperature_kelvin: 292 },
        diffractions: [
          { id: "1", temperature_kelvin: 100 },
          { id: "2", temperature_kelvin: 100 },
        ],
      },
    ],
  };

  // when
  const view = crystallographyView(crystallography);

  // then
  assert.deepEqual(
    view.datasets.map((dataset) => [
      dataset.datasetId,
      dataset.crystalId,
      dataset.ph,
      dataset.growthKelvin,
      dataset.collectionKelvin,
    ]),
    [
      ["1", "1", 5.8, 292, 100],
      ["2", "1", 5.8, 292, 100],
    ],
  );
});

test("should keep the conditions of a crystal nothing was collected from", () => {
  // given
  const crystallography = {
    crystals: [{ id: "2", growth: { temperature_kelvin: 277 } }],
  };

  // when
  const view = crystallographyView(crystallography);

  // then
  assert.equal(view.datasets.length, 1);
  assert.equal(view.datasets[0].datasetId, null);
  assert.equal(view.datasets[0].growthKelvin, 277);
});

test("should carry each crystal's own conditions onto its own rows", () => {
  // given
  const crystallography = {
    crystals: [
      { id: "1", growth: { ph: 9 }, diffractions: [{ id: "1", temperature_kelvin: 100 }] },
      { id: "2", growth: { ph: 4.5 }, diffractions: [{ id: "2", temperature_kelvin: 110 }] },
    ],
  };

  // when
  const view = crystallographyView(crystallography);

  // then
  assert.deepEqual(
    view.datasets.map((dataset) => [dataset.crystalId, dataset.ph]),
    [["1", 9], ["2", 4.5]],
  );
});

test("should leave a missing measurement empty rather than guess it", () => {
  // given -- 1EMA records a pH and a collection temperature, no growth temperature
  const crystallography = {
    crystals: [{ id: "1", growth: { ph: 8.2 }, diffractions: [{ id: "1", temperature_kelvin: 295 }] }],
  };

  // when
  const [dataset] = crystallographyView(crystallography).datasets;

  // then
  assert.equal(dataset.ph, 8.2);
  assert.equal(dataset.growthKelvin, null);
  assert.equal(dataset.collectionKelvin, 295);
});

test("should render nothing when the deposit measured nothing", () => {
  // given -- 4HHB declares a crystal and a diffraction and measures neither
  const bare = { crystals: [{ id: "1", diffractions: [{ id: "1" }] }] };

  // then
  assert.equal(crystallographyView(bare), null);
  assert.equal(crystallographyView({ crystals: [] }), null);
  assert.equal(crystallographyView(undefined), null);
});

test("should number crystals and datasets by position when ids are missing", () => {
  // given
  const crystallography = {
    crystals: [{ growth: { temperature_kelvin: 293 }, diffractions: [{ temperature_kelvin: 100 }] }],
  };

  // when
  const [dataset] = crystallographyView(crystallography).datasets;

  // then
  assert.equal(dataset.crystalId, "1");
  assert.equal(dataset.datasetId, "1");
});

test("should print temperatures in kelvins and pH as a bare number", () => {
  assert.equal(formatKelvin(100), "100 K");
  assert.equal(formatKelvin(292.15), "292.2 K");
  assert.equal(formatPH(5.8), "5.8");
});
