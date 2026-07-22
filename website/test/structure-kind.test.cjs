require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { detectStructureKind } = require("../src/lib/structureKind.ts");

test("should detect structure kind from type or filename", () => {
  assert.equal(detectStructureKind("model.pdb"), "pdb");
  assert.equal(detectStructureKind("https://example.com/4hhb.cif"), "mmcif");
  assert.equal(detectStructureKind("application/mmcif"), "mmcif");
  assert.equal(detectStructureKind("density.map"), "ccp4");
  assert.equal(detectStructureKind("electron density"), "ccp4");
  assert.equal(detectStructureKind("notes.txt"), null);
  assert.equal(detectStructureKind(undefined), null);
});
