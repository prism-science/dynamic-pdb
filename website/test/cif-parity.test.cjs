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

const compareText = (left, right) => (left < right ? -1 : left > right ? 1 : 0);

// The Go parser's answer for each file is committed as parity-golden.json (see
// lib/cif/parity_test.go). The browser preview must give the same compounds,
// review flags, rules (the decision record, compounds left out included) and
// glycans for the same file, so a depositor is shown what will be stored.
for (const [name, expected] of Object.entries(golden)) {
  test(`the preview parser agrees with the Go parser on ${name}`, () => {
    // given
    const text = fs.readFileSync(path.join(cifRoot, "testdata", name), "utf8");
    const { CIF_REFERENCE } = require("../src/lib/parse/cifReference.gen.ts");

    // when
    const facts = parseStructureFacts(text, name.endsWith(".pdb") ? "pdb" : "mmcif");

    // then
    const reviewByKey = new Map(
      facts.needsReview.map((item) => [`${item.list}:${item.id}`, item.reason]),
    );
    const sources = facts.compoundSources;
    const listed = [
      ...(facts.metadata.ligands ?? []).map((id) => ({ list: "ligands", id, rule: sources.ligands[id]?.rule })),
      ...(facts.metadata.cofactors ?? []).map((id) => ({ list: "cofactors", id, rule: sources.cofactors[id]?.rule })),
    ].map(({ list, id, rule }) => {
      const reason = reviewByKey.get(`${list}:${id}`);
      return reason === undefined
        ? { list, id, rule }
        : { list, id, rule, needs_review: true, reason };
    });
    const excluded = Object.entries(sources.excluded).map(([id, source]) => ({ list: "excluded", id, rule: source.rule }));
    const compounds = [...listed, ...excluded].sort(
      (a, b) => compareText(a.list, b.list) || compareText(a.id, b.id),
    );
    assert.deepEqual(compounds, expected.compounds);
    assert.equal(Object.keys(sources.ligands).length, (facts.metadata.ligands ?? []).length);
    assert.equal(Object.keys(sources.cofactors).length, (facts.metadata.cofactors ?? []).length);
    for (const group of [sources.ligands, sources.cofactors, sources.excluded]) {
      for (const source of Object.values(group)) {
        assert.equal(source.list_version, CIF_REFERENCE.referenceVersion);
      }
    }
    assert.deepEqual(facts.glycans, expected.glycans ?? []);
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

test("the committed reference lists match the JSON files in lib/cif", async () => {
  // given
  const { generate } = await import("../scripts/gen-cif-reference.mjs");

  // when
  const generated = generate();

  // then
  assert.deepEqual(Object.keys(generated), ["cifReference.gen.ts", "ccdCategories.gen.ts"]);
  for (const [name, contents] of Object.entries(generated)) {
    const committed = fs.readFileSync(path.join(__dirname, "..", "src", "lib", "parse", name), "utf8");
    assert.equal(committed, contents, `src/lib/parse/${name} is out of date: run \`npm run gen:cif-reference\``);
  }
});

/** The preview parser loaded once more with the approval flag of the generated
 *  lists cleared, as it runs while a new list version is a draft. The module
 *  cache is restored, so other tests keep the approved parser. */
function draftPreviewParser() {
  const referencePath = require.resolve("../src/lib/parse/cifReference.gen.ts");
  const reloaded = [
    require.resolve("../src/lib/parse/ligandRule.ts"),
    require.resolve("../src/lib/parse/structure.ts"),
  ];
  const reference = require(referencePath);
  const approvedModules = reloaded.map((modulePath) => require.cache[modulePath]);
  try {
    require.cache[referencePath].exports = {
      ...reference,
      CIF_REFERENCE: { ...reference.CIF_REFERENCE, approved: false },
    };
    for (const modulePath of reloaded) {
      delete require.cache[modulePath];
    }
    return require(reloaded[1]).parseStructureFacts;
  } finally {
    require.cache[referencePath].exports = reference;
    reloaded.forEach((modulePath, index) => {
      require.cache[modulePath] = approvedModules[index];
    });
  }
}

const BUFFER_FILE = [
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
  "HETATM SO4 D 4 S",
  "HETATM SO4 D 4 O1",
  "#",
].join("\n");

test("water is the only compound dropped while the reference lists are drafts", () => {
  // given
  const parseWithDraftLists = draftPreviewParser();

  // when
  const facts = parseWithDraftLists(BUFFER_FILE, "mmcif");

  // then
  assert.deepEqual(facts.metadata.ligands, ["GOL", "SO4"]);
  assert.deepEqual(
    facts.needsReview.map((item) => [item.id, item.reason]),
    [
      ["GOL", "compound is on the draft solvent list, which researchers have not approved: kept as a ligand instead of dropped"],
      ["SO4", "often from the buffer: confirm it is part of the sample"],
    ],
  );
});

test("buffers are dropped and ambiguous compounds flagged once the reference lists are approved", () => {
  // given
  const { CIF_REFERENCE } = require("../src/lib/parse/cifReference.gen.ts");

  // when
  const facts = parseStructureFacts(BUFFER_FILE, "mmcif");

  // then
  assert.equal(CIF_REFERENCE.approved, true, "this test describes the approved lists");
  assert.deepEqual(facts.metadata.ligands, ["SO4"]);
  assert.deepEqual(
    facts.needsReview.map((item) => [item.id, item.reason]),
    [["SO4", "often from the buffer: confirm it is part of the sample"]],
  );
});
