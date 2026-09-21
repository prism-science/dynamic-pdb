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

test("should draw stored residue metrics on the shared sequence ruler", () => {
  // given
  const [entity] = polymerEntityViews(
    [
      {
        id: "e1",
        label_entity_id: "1",
        label_asym_id: "A",
        residue_data: [
          {
            label_asym_id: "A",
            label_seq_id: 2,
            label_comp_id: "CYS",
            auth_asym_id: "X",
            auth_seq_id: 12,
            rscc: 0.924,
            b_iso: 18.5,
            occupancy: 0.86,
            conformer_count: 2,
            rmsf: 0.41,
          },
          {
            label_asym_id: "A",
            label_seq_id: 3,
            b_iso: 24.5,
            occupancy: 1,
            rscc: 0.98,
            conformer_count: 1,
            rmsf: 0.82,
          },
        ],
      },
    ],
    [{ header: ">1ABC_1|Chain X", sequence: "ACDE" }],
  );

  // when
  const tracks = sequenceTracks(entity, null);

  // then
  assert.deepEqual(
    tracks.map((track) => track.key),
    ["bfactor", "occupancy", "rscc", "conformerCount", "rmsf"],
  );
  assert.equal(tracks[1].features[0].level, 0.86);
  assert.equal(tracks[2].features[0].title, "RSCC: 0.924 | CYS 2 [auth 12] | Chain X");
  assert.equal(tracks[3].features[0].level, 1);
  assert.equal(tracks[4].features[0].level, 0.5);
});

test("should prefer stored B-factor values over the coordinate fallback", () => {
  // given
  const [entity] = polymerEntityViews(
    [
      {
        id: "e1",
        label_entity_id: "1",
        label_asym_id: "A",
        residue_data: [
          { label_asym_id: "A", label_seq_id: 1, b_iso: 70 },
          { label_asym_id: "A", label_seq_id: 2, b_iso: 90 },
        ],
      },
    ],
    [{ header: ">1ABC_1|Chain A", sequence: "ACDEF" }],
  );

  // when
  const tracks = sequenceTracks(entity, readStructure(CIF)[0]);
  const bFactors = tracks.filter((track) => track.key === "bfactor");

  // then
  assert.equal(bFactors.length, 1);
  assert.equal(bFactors[0].features[0].title, "B-factor: 70.00 Å² | Residue 1 | Chain A");
});

// One chain of eight residues, written twice: this model, and another that
// puts residue 4 well off, splits residue 2 into two conformers, and leaves
// residue 8 out altogether. A zig-zag rather than a straight run, so the
// superposition is properly determined and cannot absorb the difference.
function chainCif(rows) {
  return `data_CMP
loop_
_atom_site.group_PDB
_atom_site.label_asym_id
_atom_site.auth_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.label_comp_id
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
${rows}
#
`;
}

const NAMES = ["ALA", "CYS", "ASP", "GLU", "PHE", "GLY", "HIS", "ILE"];
const SPOTS = [
  [0, 0, 0],
  [3.8, 1.4, 0.5],
  [7.6, 0, 1.1],
  [11.4, 1.4, 0.2],
  [15.2, 0, 0.9],
  [19, 1.4, 0],
  [22.8, 0, 0.7],
  [26.6, 1.4, 0.3],
];

function atom(index, [x, y, z], alt = ".") {
  return `ATOM A A 1 ${index + 1} ${index + 1} ${NAMES[index]} CA ${alt} ${x} ${y} ${z}`;
}

const MINE = chainCif(SPOTS.map((spot, index) => atom(index, spot)).join("\n"));

const THEIRS = chainCif(
  [
    ...SPOTS.slice(0, 7).map((spot, index) =>
      atom(index, index === 3 ? [spot[0], spot[1], spot[2] + 1.5] : spot),
    ),
    // Residue 2 again, in a second conformation a tenth of an angstrom away.
    atom(1, [SPOTS[1][0] + 0.1, SPOTS[1][1], SPOTS[1][2]], "B"),
  ].join("\n"),
);

function oneEntity() {
  return polymerEntityViews(
    [{ id: "e1", label_entity_id: "1", description: "Thing" }],
    [{ header: ">1XYZ_1|Chain A|Thing", sequence: "ACDEFGHI" }],
  );
}

test("should draw the comparison rows from the other models' own coordinates", () => {
  // when
  const [chain] = sequenceChains(oneEntity(), readStructure(MINE), [
    { modelId: "m2", title: "qFit model", chains: readStructure(THEIRS) },
  ]);
  const track = (key) => chain.tracks.find((row) => row.key === key);

  // then -- one fit, on the alpha carbons the two agree about
  assert.equal(chain.agreement.fitted.length, 1);
  assert.ok(chain.agreement.fitted[0].matched >= 3);

  // the residue the other model moves is the worst on the chain, and the
  // chart -- not a row in the viewer -- is where that is drawn
  assert.equal(chain.agreement.worst.seq, 4);
  assert.ok(chain.agreement.worst.value > 1);
  assert.equal(track("departure"), undefined);
  assert.equal(track("spread"), undefined);

  // residue 8 is ours alone; nothing is theirs alone
  assert.deepEqual(
    track("onlyMine").features.map(({ start, end }) => [start, end]),
    [[8, 8]],
  );
  assert.equal(track("onlyOthers"), undefined);

  // and their split residue is named after them, on a row of its own
  const alt = chain.tracks.find((row) => row.key === "alt:m2");
  assert.equal(alt.label, "Alt · qFit model");
  assert.deepEqual(alt.features.map((feature) => feature.start), [2]);
});

test("should draw no comparison rows when there is nothing to compare with", () => {
  // when
  const [chain] = sequenceChains(oneEntity(), readStructure(MINE));

  // then
  assert.equal(chain.agreement, null);
  assert.deepEqual(
    chain.tracks.map((row) => row.key).filter((key) => key.startsWith("alt:")),
    [],
  );
  assert.equal(
    chain.tracks.find((row) => row.key === "departure"),
    undefined,
  );
});
