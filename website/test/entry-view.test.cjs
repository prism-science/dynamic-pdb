require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  entryDetails,
  entryInfoFacts,
} = require("../src/app/entries/[entryId]/entry-view.tsx");

test("should show the entry's own facts and leave crystallography to its own section", () => {
  // given
  const entry = {
    external_refs: { pdb: "1FFK" },
    details: "Additional deposition details",
    resolution: 2.4,
    method: "X-ray crystallography",
    space_group: "C 2 2 21",
    crystallography: {
      crystals: [
        {
          id: "1",
          growth: { ph: 5.8, temperature_kelvin: 292 },
          diffractions: [{ id: "1", temperature_kelvin: 100 }],
        },
      ],
    },
  };

  // when
  const facts = entryInfoFacts(entry);

  // then
  assert.equal(entryDetails(entry), "Additional deposition details");
  assert.deepEqual(
    facts.map((fact) => fact.label),
    ["PDB", "Resolution", "Method", "Space group"],
  );
});
