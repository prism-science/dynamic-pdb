require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  chainsLabel,
  groupedPolymerEntities,
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

test("should keep equal sequence chains separate and group them by sequence id", () => {
  // given
  const entities = [
    entity({
      id: "chain-a",
      protein_sequence_id: "sequence-1",
      label_entity_id: "1",
      label_asym_id: "A",
      auth_asym_id: "A",
      description: "Hemoglobin subunit alpha",
    }),
    entity({
      id: "chain-c",
      protein_sequence_id: "sequence-1",
      label_entity_id: "1",
      label_asym_id: "C",
      auth_asym_id: "C",
      description: "Hemoglobin subunit alpha",
    }),
  ];
  const storedSequence = sequence(
    "4HHB_1|Chains A, C|Hemoglobin subunit alpha",
    "VLSPADK",
  );
  storedSequence.id = "sequence-1";

  // when
  const views = polymerEntityViews(entities, [storedSequence]);
  const groups = groupedPolymerEntities(views);

  // then
  assert.deepEqual(
    views.map((view) => view.chains),
    [["A"], ["C"]],
  );
  assert.equal(groups.length, 1);
  assert.deepEqual(groups[0].chains, ["A", "C"]);
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

test("should read one molecule in several constructs as one group", () => {
  // given -- 5NX1: one inhibitor deposited as three fragments plus the enzyme
  const entities = [
    entity({
      label_entity_id: "1",
      description: "Kallikrein-6",
      source_organisms: [{ scientific_name: "Homo sapiens", ncbi_taxonomy_id: 9606 }],
      uniprot_mappings: [{ accession: "Q92876", source: "sifts" }],
    }),
    entity({
      label_entity_id: "2",
      description: "Amyloid-beta A4 protein",
      source_organisms: [{ scientific_name: "Homo sapiens", ncbi_taxonomy_id: 9606 }],
      uniprot_mappings: [{ accession: "P05067", source: "sifts" }],
    }),
    entity({
      label_entity_id: "3",
      description: "Amyloid-beta A4 protein",
      source_organisms: [{ scientific_name: "Homo sapiens", ncbi_taxonomy_id: 9606 }],
      uniprot_mappings: [{ accession: "P05067", source: "sifts" }],
    }),
  ];
  const sequences = [
    sequence("5NX1_1|Chain A|Kallikrein-6", "A".repeat(223)),
    sequence("5NX1_2|Chain B|Amyloid-beta A4 protein", "A".repeat(25)),
    sequence("5NX1_3|Chain C|Amyloid-beta A4 protein", "A".repeat(56)),
  ];

  // when
  const groups = groupedPolymerEntities(polymerEntityViews(entities, sequences));

  // then
  assert.equal(groups.length, 2);
  assert.deepEqual(
    groups.map((group) => [group.name, group.chains, group.residues, group.entityCount]),
    [
      ["Kallikrein-6", ["A"], 223, 1],
      ["Amyloid-beta A4 protein", ["B", "C"], 81, 2],
    ],
  );
});

test("should group by molecule name when there is no uniprot mapping", () => {
  // given
  const entities = [
    entity({ label_entity_id: "1", description: "DNA polymerase" }),
    entity({ label_entity_id: "2", description: "DNA polymerase" }),
    entity({ label_entity_id: "3", description: "Thioredoxin" }),
  ];

  // when
  const groups = groupedPolymerEntities(polymerEntityViews(entities, []));

  // then
  assert.deepEqual(groups.map((group) => group.entityCount), [2, 1]);
});

test("should keep every distinct organism of a group and no duplicates", () => {
  // given
  const entities = [
    entity({
      label_entity_id: "1",
      description: "Chimera",
      source_organisms: [{ scientific_name: "Homo sapiens" }],
      uniprot_mappings: [{ accession: "P00001", source: "sifts" }],
    }),
    entity({
      label_entity_id: "2",
      description: "Chimera",
      source_organisms: [
        { scientific_name: "Homo sapiens" },
        { scientific_name: "Escherichia coli" },
      ],
      uniprot_mappings: [{ accession: "P00001", source: "sifts" }],
    }),
  ];

  // when
  const [group] = groupedPolymerEntities(polymerEntityViews(entities, []));

  // then
  assert.deepEqual(
    group.organisms.map((organism) => organism.scientific_name),
    ["Homo sapiens", "Escherichia coli"],
  );
});

test("should carry the artifact of the matched sequence, and nothing when none matched", () => {
  // given -- two FASTA files, one record each: a chain must lead to the file it
  // was read out of, not to whichever file came first.
  const entities = [
    entity({ label_entity_id: "1", description: "Kallikrein-6" }),
    entity({ label_entity_id: "2", description: "Amyloid-beta A4 protein" }),
    entity({ label_entity_id: "3", description: "tRNA-Phe" }),
  ];
  const sequences = [
    sequence("5NX1_1|Chain A|Kallikrein-6", "LVHGGPCDKT", "artifact-kallikrein"),
    sequence("5NX1_2|Chain B|Amyloid-beta A4 protein", "YVDYK", "artifact-appi"),
  ];

  // when
  const views = polymerEntityViews(entities, sequences);

  // then
  assert.equal(views[0].sequenceArtifactId, "artifact-kallikrein");
  assert.equal(views[1].sequenceArtifactId, "artifact-appi");
  assert.equal(views[2].sequenceArtifactId, null);
});

test("should carry residue data from its own polymer chain", () => {
  // given
  const residues = [
    { label_asym_id: "A", label_seq_id: 7, rscc: 0.97 },
  ];

  // when
  const [view] = polymerEntityViews(
    [entity({ label_entity_id: "1", label_asym_id: "A", residue_data: residues })],
    [sequence("1ABC_1|Chain A", "ACDEFGH")],
  );

  // then
  assert.deepEqual(view.residueData, residues);
});

function entity(overrides) {
  return {
    id: `entity-${overrides.label_entity_id ?? "x"}`,
    protein_sequence_id: "",
    source_organisms: [],
    uniprot_mappings: [],
    residue_data: [],
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function sequence(header, residues, artifactId = "artifact-1") {
  return {
    id: `sequence-${header}`,
    source_artifact_id: artifactId,
    record_index: 0,
    header,
    sequence: residues,
    created_at: "2026-01-01T00:00:00Z",
  };
}
