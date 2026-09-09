require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  residueLetters,
  rulerTicks,
  sequenceChains,
  sequenceLength,
  sequenceTracks,
} = require("../src/lib/sequence-tracks.ts");
const { polymerEntityViews } = require("../src/lib/polymer-entities.ts");
const { readStructure } = require("../src/lib/structure-tracks.ts");

// Entity 1 in two chains, entity 2 in one.
const CIF = `data_TEST
loop_
_struct_conf.conf_type_id
_struct_conf.beg_label_asym_id
_struct_conf.beg_label_seq_id
_struct_conf.end_label_seq_id
HELX_P A 2 4
#
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.label_alt_id
_atom_site.B_iso_or_equiv
ATOM A A 1 1 . 10.0
ATOM A A 1 2 . 20.0
ATOM A A 1 4 . 30.0
ATOM B B 1 1 . 40.0
ATOM C C 2 1 . 50.0
#
`;

function views() {
  return polymerEntityViews(
    [
      { id: "e1", label_entity_id: "1", description: "Glucanase" },
      { id: "e2", label_entity_id: "2", description: "Lysozyme" },
    ],
    [
      { header: ">1ABC_1|Chains A, B|Glucanase", sequence: "ACDEF" },
      { header: ">1ABC_2|Chain C|Lysozyme", sequence: "GHIK" },
    ],
  );
}

test("should offer one entry per chain the model's coordinates contain", () => {
  // when
  const chains = sequenceChains(views(), readStructure(CIF));

  // then
  assert.deepEqual(
    chains.map((chain) => `${chain.entityId}${chain.chainId}`),
    ["1A", "1B", "2C"],
  );
  assert.ok(chains.every((chain) => chain.modelled));
});

test("should give each chain of one entity its own coordinate rows", () => {
  // when
  const chains = sequenceChains(views(), readStructure(CIF));
  const rows = (chain) => chain.tracks.map((track) => track.key);

  // then: A holds residues 1, 2 and 4 of five, and carries the helix; B holds
  // only residue 1, and no helix is recorded for it
  assert.ok(rows(chains[0]).includes("secondary"));
  assert.ok(rows(chains[0]).includes("bfactor"));
  assert.deepEqual(
    chains[0].tracks
      .find((track) => track.key === "unobserved")
      .features.map(({ start, end }) => ({ start, end })),
    [
      { start: 3, end: 3 },
      { start: 5, end: 5 },
    ],
  );
  assert.ok(!rows(chains[1]).includes("secondary"));
});

test("should fall back to the chains the entry names when a model has none", () => {
  // when
  const chains = sequenceChains(views(), []);

  // then
  assert.deepEqual(
    chains.map((chain) => chain.chainId),
    ["A", "B", "C"],
  );
  assert.ok(chains.every((chain) => chain.modelled === false));
  assert.ok(
    chains.every((chain) =>
      chain.tracks.every((track) => track.key !== "bfactor"),
    ),
  );
});

test("should carry the residue letters of the chain's own entity", () => {
  // when
  const chains = sequenceChains(views(), readStructure(CIF));

  // then
  assert.equal(chains[0].sequence, "ACDEF");
  assert.equal(chains[0].length, 5);
  assert.equal(chains[2].sequence, "GHIK");
  assert.equal(chains[2].moleculeName, "Lysozyme");
});

test("should leave an entity with no sequence and no residue count out", () => {
  // given
  const [entity] = polymerEntityViews(
    [{ id: "e1", label_entity_id: "1", description: "Unknown" }],
    [],
  );

  // when / then
  assert.equal(sequenceLength(entity), 0);
  assert.deepEqual(sequenceChains([entity], []), []);
});

test("should read the stored sequence without the file's line wrapping", () => {
  // given
  const [entity] = polymerEntityViews(
    [{ id: "e1", label_entity_id: "1" }],
    [{ header: ">1ABC_1|Chain A", sequence: "ACD\nEF\n" }],
  );

  // when / then
  assert.equal(residueLetters(entity), "ACDEF");
  assert.equal(sequenceLength(entity), 5);
});

test("should keep the ruler within the number of labels it has room for", () => {
  // when
  const fitted = rulerTicks(310);
  const zoomed = rulerTicks(310, 74);

  // then
  assert.deepEqual(fitted, [50, 100, 150, 200, 250]);
  assert.equal(zoomed[0], 5);
  assert.ok(zoomed.length > fitted.length);
  assert.ok(zoomed.every((tick) => tick < 310));
});

test("should draw no chain row: the chain is chosen above the viewer", () => {
  // given
  const [entity] = views();

  // when
  const keys = sequenceTracks(entity, null).map((track) => track.key);

  // then
  assert.ok(!keys.includes("chain"));
});

test("should draw no UniProt row: the mapping records an accession, not a range", () => {
  // given: a construct line with two numbers and a dash in it, which the row
  // used to read as the mapped range
  const [entity] = polymerEntityViews(
    [
      {
        id: "e1",
        label_entity_id: "1",
        construct: "UNP residues 24-333, C-terminal His6 tag",
        uniprot_mappings: [{ accession: "P00698", source: "sifts" }],
      },
    ],
    [{ header: ">1ABC_1|Chain A", sequence: "ACDEFGHIK" }],
  );

  // when
  const keys = sequenceTracks(entity, null).map((track) => track.key);

  // then
  assert.ok(!keys.some((key) => key.startsWith("uniprot")));
});
