require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { altlocEnsembleFile } = require("../src/lib/altloc-ensemble.ts");
const { altlocEnsembleMembers, modelRole } = require("../src/lib/hetstar-support.ts");
const { altlocEnsembleDpdbConfig } = require("../src/lib/hetstar.ts");
const { parseStructureFacts } = require("../src/lib/parse/structure.ts");
const { toFetchableUrl } = require("../vendor/hetstar/packages/hetstar/src/lib/dpdb/client.ts");

const HEADER = `data_TEST
_entry.id TEST
#
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.auth_asym_id
_atom_site.occupancy
_atom_site.pdbx_PDB_model_num
`;

function atomRows(residues, letters, model = 1) {
  const rows = [];
  for (let residue = 1; residue <= residues; residue += 1) {
    for (const letter of letters) {
      rows.push(`ATOM ${rows.length + 1} C CA ${letter} GLY A ${residue} ${residue} A 0.5 ${model}`);
    }
  }
  return rows.join("\n");
}

function cif(atomSite, trailer = "") {
  return `${HEADER}${atomSite}\n#\n_cell.length_a 10\n${trailer}`;
}

function atomLines(text) {
  return text.split("\n").filter((line) => /^(ATOM|HETATM) /.test(line));
}

test("should give each whole-chain altloc letter its own MODEL", () => {
  // given
  const text = cif(atomRows(2, ["A", "B", "C"]));

  // when
  const file = altlocEnsembleFile(text);

  // then
  assert.equal(file.kind, "converted");
  assert.deepEqual(file.letters, ["A", "B", "C"]);
  const rows = atomLines(file.text).map((line) => line.split(" "));
  assert.equal(rows.length, 6);
  assert.deepEqual(rows.map((row) => row[11]), ["1", "1", "2", "2", "3", "3"]);
  assert.deepEqual(rows.map((row) => row[1]), ["1", "2", "3", "4", "5", "6"]);
  assert.ok(rows.every((row) => row[4] === "."));
  assert.match(file.text, /_cell\.length_a 10/);
});

test("should keep shared atoms in every frame and leave a water's own letter alone", () => {
  // given: a water in every state, and a water under a letter no polymer residue has
  const text = cif(
    `${atomRows(1, ["A", "B"])}\nHETATM 8 O O . HOH B . 101 B 0.34 1\nHETATM 9 O O c HOH B . 102 B 0.13 1`,
  );

  // when
  const file = altlocEnsembleFile(text);

  // then
  assert.deepEqual(file.letters, ["A", "B"]);
  const rows = atomLines(file.text).map((line) => line.split(" "));
  assert.equal(rows.filter((row) => row[8] === "101").length, 2);
  assert.deepEqual(
    rows.filter((row) => row[8] === "102").map((row) => [row[4], row[11]]),
    [["c", "1"], ["c", "2"]],
  );
});

test("should keep a quoted atom name whole when it writes the rows back", () => {
  // given
  const text = cif(
    `ATOM 1 C "C1'" A GLY A 1 1 A 0.5 1\nATOM 2 C "C1'" B GLY A 1 1 A 0.5 1`,
  );

  // when
  const file = altlocEnsembleFile(text);

  // then
  assert.equal(file.kind, "converted");
  assert.ok(atomLines(file.text).every((line) => line.split(" ")[3] === `"C1'"`));
});

test("should drop anisotropic rows that point at the renumbered atoms", () => {
  // given
  const text = cif(
    atomRows(1, ["A", "B"]),
    "#\nloop_\n_atom_site_anisotrop.id\n_atom_site_anisotrop.U[1][1]\n1 0.1\n2 0.1\n#\n_end.id 1\n",
  );

  // when
  const file = altlocEnsembleFile(text);

  // then
  assert.doesNotMatch(file.text, /_atom_site_anisotrop/);
  assert.match(file.text, /_end\.id 1/);
});

test("should leave a multi-MODEL ensemble as it is", () => {
  // given
  const text = cif(`${atomRows(1, ["."], 1)}\n${atomRows(1, ["."], 2)}`);

  // when
  const file = altlocEnsembleFile(text);

  // then
  assert.deepEqual(file, { kind: "multi-model" });
});

test("should refuse letters that do not cover every polymer residue", () => {
  // given: the shape of a qFit model, with alternate positions on some residues
  const text = cif(`${atomRows(2, ["A", "B"])}\nATOM 9 C CA . GLY A 3 3 A 1.0 1`);

  // when
  const file = altlocEnsembleFile(text);

  // then
  assert.deepEqual(file, { kind: "not-an-altloc-ensemble" });
});

test("should count members in the form's parser as lib/cif counts them", () => {
  // given
  const altlocs = cif(atomRows(2, ["A", "B", "C"]));
  const models = cif(`${atomRows(2, ["."], 1)}\n${atomRows(2, ["."], 2)}`);
  const partial = cif(`${atomRows(2, ["A", "B"])}\nATOM 9 C CA . GLY A 3 3 A 1.0 1`);

  // when
  const counts = [altlocs, models, partial].map(
    (text) => parseStructureFacts(text, "mmcif").metadata.member_count,
  );

  // then
  assert.deepEqual(counts, [3, 2, undefined]);
});

test("should send only altloc ensembles through the frames", () => {
  // given
  const altlocEnsemble = { model_type: "Ensemble", member_count: 6, altloc_fraction: 1 };
  const multiModel = { model_type: "Ensemble", member_count: 8, altloc_fraction: 0 };
  const uncounted = { model_type: "Ensemble", altloc_fraction: 1 };
  const qfit = { model_type: "Multiconformer", altloc_fraction: 0.8 };

  // when
  const members = [altlocEnsemble, multiModel, uncounted, qfit].map(altlocEnsembleMembers);

  // then
  assert.deepEqual(members, [6, null, null, null]);
});

// These two pin what the frames rely on inside the vendored viewer, so a submodule bump that
// changes either fails here instead of silently loading the deposited file.
test("should let the viewer fetch the relative frames path unchanged", () => {
  // when
  const url = toFetchableUrl("/dpdb-ensemble-frames/dpdb_a/dpdb_a_m_005");

  // then
  assert.equal(url, "/dpdb-ensemble-frames/dpdb_a/dpdb_a_m_005");
});

test("should keep the file proxy and swap only the catalogue for frames", () => {
  // when
  const config = altlocEnsembleDpdbConfig({ apiBase: "/api/v1", fileProxy: "/dpdb-file" }, "dpdb_a", "dpdb_a_m_005");

  // then
  assert.equal(config.apiBase, "/dpdb-api-ensemble/dpdb_a/dpdb_a_m_005/v1");
  assert.equal(config.fileProxy, "/dpdb-file");
});

test("should not take a model titled from RCSB for the deposited model", () => {
  // given
  const model = { title: "6-conformation model of SARS-CoV-2 NSP3 macrodomain", metadata: { model_type: "Ensemble" } };

  // when
  const role = modelRole(model);

  // then
  assert.equal(role, "ensemble");
});

const {
  buildArchiveModelInput,
  reRefinementTargets,
  suggestedModelType,
} = require("../src/app/components/entry-form/archiveModel.ts");
const { lookupReviewMessages } = require("../src/app/components/entry-form/lookupReview.ts");
const { createModelRequestBody } = require("../src/lib/api/entries.ts");
const { summaryModelFacts } = require("../src/app/entries/[entryId]/entry-view.tsx");

function archiveLookup(overrides = {}) {
  return {
    pdbId: "44PS",
    title: "6-conformation model",
    coordinateFileURL: "https://files.rcsb.org/download/44PS.cif",
    structureFactorsURL: "https://files.rcsb.org/download/44PS-sf.cif",
    metadata: { authors: ["Passmore, S.K."], altloc_fraction: 1, ligands: [] },
    memberCount: 6,
    metrics: [{ key: "r_free", value: 0.1, software_name: "PHENIX" }],
    relatedEntries: [{ db_name: "PDB", db_id: "7KR0", content_type: "re-refinement" }],
    ...overrides,
  };
}

test("should send an archive deposition with its origin, relationship and RCSB file", () => {
  // given
  const lookup = archiveLookup();

  // when
  const { model } = createModelRequestBody(
    buildArchiveModelInput(lookup, { modelType: "Ensemble", purpose: "Refinement" }),
  );

  // then
  assert.equal(model.metadata.model_type, "Ensemble");
  assert.equal(model.metadata.origin, "archive");
  assert.equal(model.metadata.member_count, 6);
  assert.deepEqual(model.metadata.external_refs, { pdb: "44PS" });
  assert.deepEqual(model.metadata.related_entries, lookup.relatedEntries);
  const file = model.artifacts.find((artifact) => artifact.type === "model");
  assert.equal(file.uri, lookup.coordinateFileURL);
  assert.equal(file.format, "cif");
  assert.equal(model.primary_artifact_id, file.id);
  assert.equal(model.metrics[0].key, "r_free");
});

test("should not send a member count when the depositor picks another type", () => {
  // when
  const { model } = createModelRequestBody(
    buildArchiveModelInput(archiveLookup(), { modelType: "Multiconformer", purpose: "Refinement" }),
  );

  // then
  assert.equal(model.metadata.member_count, undefined);
});

test("should suggest a type from what the file holds", () => {
  // when
  const types = [
    archiveLookup(),
    archiveLookup({ memberCount: null }),
    archiveLookup({ memberCount: null, metadata: { authors: [], ligands: [] } }),
  ].map(suggestedModelType);

  // then
  assert.deepEqual(types, ["Ensemble", "Multiconformer", "Single Conformer"]);
});

test("should read only PDB re-refinement rows as what a deposition re-refines", () => {
  // when
  const targets = reRefinementTargets([
    { db_name: "PDB", db_id: "7kr0", content_type: "re-refinement" },
    { db_name: "PDB", db_id: "5KXL", content_type: "unspecified" },
    { db_name: "EMDB", db_id: "EMD-1", content_type: "re-refinement" },
  ]);

  // then
  assert.deepEqual(targets, ["7KR0"]);
});

test("should point the new-entry form to the registered entry of the original deposition", () => {
  // when
  const messages = lookupReviewMessages("44PS", {
    entry: { conflicts: [], warnings: [] },
    polymerEntities: [],
    warnings: [],
    reRefinementOf: { pdbId: "7KR0", entryId: "dpdb_abc" },
  });

  // then
  assert.ok(messages.some((message) => message.message.includes("/entries/dpdb_abc/models/new")));
});

test("should show a deposition's PDB ID, what it re-refines and its members", () => {
  // given
  const metadata = {
    external_refs: { pdb: "44PS" },
    related_entries: [
      { db_name: "PDB", db_id: "7KR0", content_type: "re-refinement" },
      { db_name: "PDB", db_id: "5KXL", content_type: "unspecified" },
    ],
    member_count: 6,
  };

  // when
  const facts = summaryModelFacts(metadata, null, { entryPdbId: "7KR0" });

  // then
  assert.deepEqual(
    facts.map((fact) => [fact.label, fact.value]),
    [
      ["PDB", "44PS"],
      ["Re-refinement of", "7KR0 (this entry)"],
      ["Members", "6"],
    ],
  );
});

test("should not repeat the entry's PDB ID on the archive's own model", () => {
  // when
  const facts = summaryModelFacts({ external_refs: { pdb: "7KR0" } }, null, { entryPdbId: "7KR0" });

  // then
  assert.deepEqual(facts, []);
});
