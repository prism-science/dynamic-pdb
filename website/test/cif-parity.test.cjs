require("./register.cjs");

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const { parseStructureFacts } = require("../src/lib/parse/structure.ts");

const cifRoot = path.resolve(__dirname, "..", "..", "lib", "cif");
const golden = JSON.parse(
  fs.readFileSync(path.join(cifRoot, "testdata", "parity-golden.json"), "utf8"),
);

// The Go parser's answer for each file is committed as parity-golden.json (see
// lib/cif/parity_test.go). The browser preview must give the same compounds and
// review flags for the same file, so a depositor is shown what will be stored.
for (const [name, expected] of Object.entries(golden)) {
  test(`the preview parser agrees with the Go parser on ${name}`, () => {
    // given
    const text = fs.readFileSync(path.join(cifRoot, "testdata", name), "utf8");

    // when
    const facts = parseStructureFacts(text, "mmcif");

    // then
    const reviewByKey = new Map(
      facts.needsReview.map((item) => [`${item.list}:${item.id}`, item.reason]),
    );
    const compounds = [
      ...(facts.metadata.ligands ?? []).map((id) => ({ list: "ligands", id })),
      ...(facts.metadata.cofactors ?? []).map((id) => ({ list: "cofactors", id })),
    ]
      .map(({ list, id }) => {
        const reason = reviewByKey.get(`${list}:${id}`);
        return reason === undefined
          ? { list, id }
          : { list, id, needs_review: true, reason };
      })
      .sort((a, b) => a.list.localeCompare(b.list) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
    assert.deepEqual(compounds, expected.compounds);
    assert.equal(facts.metadata.atom_count ?? 0, expected.atom_count);
    assert.equal(facts.metadata.modeled_residues ?? 0, expected.modeled_residues);
    assert.equal(facts.metadata.unique_protein_chains ?? 0, expected.unique_protein_chains);
  });
}

test("the preview and the CLI agree that 4HHB has HEM as a cofactor and phosphate as a flagged ligand", () => {
  // given
  const text = fs.readFileSync(path.join(cifRoot, "testdata", "4HHB.cif"), "utf8");

  // when
  const facts = parseStructureFacts(text, "mmcif");

  // then
  assert.deepEqual(facts.metadata.cofactors, ["HEM"]);
  assert.deepEqual(facts.metadata.ligands, ["PO4"]);
  assert.deepEqual(
    facts.needsReview.map((item) => `${item.list}:${item.id}`),
    ["ligands:PO4"],
  );
});

test("the committed reference lists match lib/cif/cofactors.json and solvents.json", async () => {
  // given
  const { generate } = await import("../scripts/gen-cif-reference.mjs");
  const committed = fs.readFileSync(
    path.join(__dirname, "..", "src", "lib", "parse", "cifReference.gen.ts"),
    "utf8",
  );

  // when
  const generated = generate();

  // then
  assert.equal(
    committed,
    generated,
    "src/lib/parse/cifReference.gen.ts is out of date: run `npm run gen:cif-reference`",
  );
});

test("water is the only compound dropped while the reference lists are drafts", () => {
  // given
  const { CIF_REFERENCE } = require("../src/lib/parse/cifReference.gen.ts");
  const text = [
    "data_X",
    "loop_",
    "_atom_site.group_PDB",
    "_atom_site.label_comp_id",
    "_atom_site.label_asym_id",
    "_atom_site.auth_seq_id",
    "_atom_site.label_atom_id",
    "ATOM ALA A 1 CA",
    "HETATM HOH B 2 O",
    "HETATM GOL C 3 C1",
    "HETATM GOL C 3 C2",
    "#",
  ].join("\n");

  // when
  const facts = parseStructureFacts(text, "mmcif");

  // then
  assert.equal(CIF_REFERENCE.approved, false, "this test describes the draft gate");
  assert.deepEqual(facts.metadata.ligands, ["GOL"]);
  assert.deepEqual(facts.needsReview.map((item) => item.id), ["GOL"]);
});
