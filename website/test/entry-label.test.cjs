require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { formatEntryLabel } = require("../src/lib/entry-label.ts");

test("should format an entry label from its PDB reference and Dynamic PDB ID", () => {
  assert.equal(
    formatEntryLabel({
      id: "dpdb_c7oclw12",
      title: "Legacy entry title",
      external_refs: { pdb: " 7b3h " },
    }),
    "PDB 7B3H | dpdb_c7oclw12",
  );
});

test("should fall back to the entry title when the PDB reference is missing", () => {
  assert.equal(
    formatEntryLabel({ id: "dpdb_c7oclw12", title: "Custom entry" }),
    "Custom entry | dpdb_c7oclw12",
  );
});
