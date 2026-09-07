require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  chainsLabel,
  hasEntityDetails,
  mutationTokens,
  organismLabel,
  polymerEntityViews,
  uniProtSourceLabel,
} = require("../src/lib/polymer-entities.ts");
const { plainSequence, sequenceLines } = require("../src/lib/fasta.ts");

test("should join a polymer entity with the sequence record named after it", () => {
  // given
  const entities = [
    entity({
      label_entity_id: "1",
      description: "Kallikrein-6",
      source_organisms: [
        { scientific_name: "Homo sapiens", ncbi_taxonomy_id: 9606 },
      ],
      uniprot_mappings: [
        { accession: "Q92876", source: "sifts", unp_release: "2025_03" },
      ],
    }),
  ];
  const sequences = [
    sequence("5NX1_1|Chain A|Kallikrein-6|Homo sapiens (9606)", "LVHGGPCDKT"),
    sequence("5NX1_2|Chain B|Amyloid-beta A4 protein|Homo sapiens (9606)", "YVDYK"),
  ];

  // when
  const [view] = polymerEntityViews(entities, sequences);

  // then
  assert.equal(view.name, "Kallikrein-6");
  assert.deepEqual(view.chains, ["A"]);
  assert.equal(view.residues, 10);
  assert.equal(view.sequence, "LVHGGPCDKT");
  assert.equal(view.uniprotMappings[0].unp_release, "2025_03");
});

test("should read every chain of a record that lists more than one", () => {
  // given
  const entities = [
    entity({ label_entity_id: "1", description: "Hemoglobin subunit alpha" }),
  ];
  const sequences = [
    sequence("4HHB_1|Chains A, C|Hemoglobin subunit alpha", "VLSPADK"),
  ];

  // when
  const [view] = polymerEntityViews(entities, sequences);

  // then
  assert.deepEqual(view.chains, ["A", "C"]);
  assert.equal(chainsLabel(view.chains), "Chains A, C");
  assert.equal(chainsLabel(["A"]), "Chain A");
  assert.equal(chainsLabel([]), null);
});

test("should keep the author chain label a record spells out", () => {
  // given
  const entities = [entity({ label_entity_id: "3" })];
  const sequences = [
    sequence("5NX1_3|Chain C[auth D]|Amyloid-beta A4 protein", "AMISR"),
  ];

  // when
  const [view] = polymerEntityViews(entities, sequences);

  // then
  assert.deepEqual(view.chains, ["C[auth D]"]);
});

test("should leave chains and residues unknown when no record matches the entity", () => {
  // given
  const entities = [entity({ label_entity_id: "2", description: "Nanobody" })];
  const sequences = [sequence("some-upload.fasta record", "QVQLQ")];

  // when
  const [view] = polymerEntityViews(entities, sequences);

  // then
  assert.deepEqual(view.chains, []);
  assert.equal(view.residues, null);
  assert.equal(view.sequence, null);
});

test("should order entities by number rather than by text", () => {
  // given
  const entities = [
    entity({ label_entity_id: "10", description: "Ten" }),
    entity({ label_entity_id: "2", description: "Two" }),
    entity({ label_entity_id: "1", description: "One" }),
  ];

  // when
  const views = polymerEntityViews(entities, []);

  // then
  assert.deepEqual(
    views.map((view) => view.entityId),
    ["1", "2", "10"],
  );
});

test("should name an entity after its number when the deposit gives no description", () => {
  // given
  const entities = [entity({ label_entity_id: "3", description: "   " })];

  // when
  const [view] = polymerEntityViews(entities, []);

  // then
  assert.equal(view.name, "Entity 3");
});

test("should split mutations into substitutions only when the field is a plain list", () => {
  // given / when / then
  assert.deepEqual(mutationTokens("K986P, V987P, R682G"), [
    "K986P",
    "V987P",
    "R682G",
  ]);
  assert.deepEqual(mutationTokens("Ala123Gly"), ["Ala123Gly"]);
  assert.equal(mutationTokens("C to S"), null);
  assert.equal(mutationTokens("YES"), null);
  assert.equal(mutationTokens("   "), null);
});

test("should carry the raw mutation text alongside the parsed substitutions", () => {
  // given
  const entities = [
    entity({ label_entity_id: "1", mutations: " K986P, V987P " }),
    entity({ label_entity_id: "2", mutations: "engineered disulfide" }),
  ];

  // when
  const [first, second] = polymerEntityViews(entities, []);

  // then
  assert.deepEqual(first.mutations, ["K986P", "V987P"]);
  assert.equal(first.mutationsText, "K986P, V987P");
  assert.equal(second.mutations, null);
  assert.equal(second.mutationsText, "engineered disulfide");
});

test("should offer no disclosure for an entity with nothing behind the row", () => {
  // given
  const [bare] = polymerEntityViews(
    [entity({ label_entity_id: "1", description: "Peptide" })],
    [],
  );
  const [referenced] = polymerEntityViews(
    [
      entity({
        label_entity_id: "1",
        description: "Peptide",
        uniprot_mappings: [{ accession: "P05067", source: "struct_ref" }],
      }),
    ],
    [],
  );

  // then
  assert.equal(hasEntityDetails(bare), false);
  assert.equal(hasEntityDetails(referenced), true);
});

test("should label organisms and the provenance of an accession", () => {
  // given / when / then
  assert.equal(
    organismLabel([
      { scientific_name: "Homo sapiens" },
      { scientific_name: "Escherichia coli" },
    ]),
    "Homo sapiens, Escherichia coli",
  );
  assert.equal(organismLabel([]), null);
  assert.equal(uniProtSourceLabel("sifts"), "SIFTS");
  assert.equal(uniProtSourceLabel("struct_ref"), "Depositor");
});

test("should lay a sequence out in groups of ten, sixty residues to a line", () => {
  // given
  const residues = "A".repeat(65);

  // when
  const lines = sequenceLines(residues);

  // then
  assert.equal(lines.length, 2);
  assert.equal(lines[0], Array(6).fill("A".repeat(10)).join(" "));
  assert.equal(lines[1], "AAAAA");
});

test("should strip the reading spaces when a sequence is copied", () => {
  // given
  const printed = sequenceLines("vlspadktnvkaawgkvgahageygaeale").join("\n");

  // when
  const copied = plainSequence(printed);

  // then
  assert.equal(copied, "VLSPADKTNVKAAWGKVGAHAGEYGAEALE");
});

function entity(overrides) {
  return {
    id: `entity-${overrides.label_entity_id ?? "x"}`,
    source_organisms: [],
    uniprot_mappings: [],
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function sequence(header, residues) {
  return {
    id: `sequence-${header}`,
    source_artifact_id: "artifact-1",
    record_index: 0,
    header,
    sequence: residues,
    created_at: "2026-01-01T00:00:00Z",
  };
}
