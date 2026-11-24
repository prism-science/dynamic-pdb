require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { entryCardHref, selectedModel, selectedModelLine } = require("../src/lib/entry-search-match.ts");

test("should open the model a PDB ID search selected, the first matched model", () => {
  const entry = {
    id: "dpdb_7kr0abcd",
    matched_models: [
      { model_id: "dpdb_7kr0abcd_m_002", pdb_id: "44ps" },
      { model_id: "dpdb_7kr0abcd_m_005", pdb_id: "8SC1" },
    ],
  };

  assert.deepEqual(selectedModel(entry), { model_id: "dpdb_7kr0abcd_m_002", pdb_id: "44ps" });
  assert.equal(entryCardHref(entry), "/entries/dpdb_7kr0abcd/models/dpdb_7kr0abcd_m_002");
  assert.equal(selectedModelLine(entry), "Opens model PDB 44PS");
});

test("should open the entry when no model was selected or the list did not come from a search", () => {
  for (const entry of [
    { id: "dpdb_7kr0abcd", matched_models: [] },
    { id: "dpdb_7kr0abcd" },
    { id: "dpdb_7kr0abcd", matched_models: null },
  ]) {
    assert.equal(selectedModel(entry), null);
    assert.equal(entryCardHref(entry), "/entries/dpdb_7kr0abcd");
    assert.equal(selectedModelLine(entry), null);
  }
});
