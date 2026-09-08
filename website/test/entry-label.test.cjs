require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  entryIdentity,
  formatEntryLabel,
} = require("../src/lib/entry-label.ts");

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

test("should split an entry's identity into the code and the id", () => {
  // when
  const withPDB = entryIdentity({
    id: "dpdb_c7oclw12",
    title: "Legacy entry title",
    external_refs: { pdb: " 7b3h " },
  });
  const withTitle = entryIdentity({ id: "dpdb_c7oclw12", title: "Custom entry" });
  const bare = entryIdentity({ id: "dpdb_c7oclw12", title: null });

  // then
  assert.deepEqual(withPDB, { name: "PDB 7B3H", id: "dpdb_c7oclw12" });
  assert.deepEqual(withTitle, { name: "Custom entry", id: "dpdb_c7oclw12" });
  assert.deepEqual(bare, { name: null, id: "dpdb_c7oclw12" });
});
