require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { emptyExtractedFile, readExtraction } = require("../src/app/components/entry-form/fileExtraction.ts");
const { askWithRetry } = require("../src/app/components/entry-form/useFileExtraction.ts");
const {
  entryFileBadges,
  entryFileMessages,
  entryValueSourcesRequest,
  pdbSuggestion,
  withEntryFileValues,
} = require("../src/app/components/entry-form/entryFileFills.ts");
const { emptyEntryMetadataDraft } = require("../src/app/components/entry-form/types.ts");
const { buildCreateEntryInput } = require("../src/app/components/entry-form/helpers.ts");

// The forms read an uploaded mmCIF on the server (POST /v1/files/extract):
// the response's values, the retry when the server is busy, and what the New
// entry form fills from them.

test("should read every value the server returns by kind", () => {
  // given
  const value = (name, item) => ({ name, value: item, source: "file:abc", needs_review: false });

  // when
  const file = readExtraction({
    source: "file:abc",
    reference_version: "v",
    bytes_read: 1,
    object_size: 1,
    truncated: false,
    values: [
      value("entry_id", "1ABC"),
      value("authors", ["Doe, J."]),
      value("r_work", 0.19),
      value("resolution", 1.8),
      value("method", "X-RAY DIFFRACTION"),
      value("ligands", [{ id: "SO4", needs_review: true, review_reason: "often from the buffer" }, { id: "ATP" }]),
      value("mutations", [{ entity_id: "1", accession: "P00698", mutations: ["C54T"], original: "C54T" }]),
      value("compound_sources", {}),
    ],
    warnings: [{ detail: "parser limit: time budget exceeded" }],
  });

  // then
  assert.equal(file.entryId, "1ABC");
  assert.deepEqual(file.authors, ["Doe, J."]);
  assert.equal(file.rWork, 0.19);
  assert.equal(file.rFree, null);
  assert.equal(file.resolution, 1.8);
  assert.equal(file.method, "X-RAY DIFFRACTION");
  assert.deepEqual(file.ligands, [
    { id: "SO4", reviewReason: "often from the buffer" },
    { id: "ATP", reviewReason: null },
  ]);
  assert.deepEqual(file.mutations, [
    { entityId: "1", accession: "P00698", mutations: ["C54T"], original: "C54T", needsReview: false },
  ]);
  assert.deepEqual(file.warnings, ["parser limit: time budget exceeded"]);
});

test("should refuse a value the API does not document", () => {
  // given
  const extraction = {
    source: "file:abc", reference_version: "v", bytes_read: 1, object_size: 1, truncated: false,
    values: [{ name: "guessed", value: 1, source: "file:abc", needs_review: false }],
    warnings: [],
  };

  // when / then
  assert.throws(() => readExtraction(extraction), /unknown extracted value: guessed/);
});

test("should ask once more when the server is busy, and keep the second answer", async () => {
  // given
  const answers = [{ status: "busy" }, { status: "off" }];
  let asked = 0;

  // when
  const result = await askWithRetry(async () => answers[asked++], 1);

  // then
  assert.equal(asked, 2);
  assert.deepEqual(result, { status: "off" });
});

test("should report a failure when the action cannot be reached", async () => {
  // when
  const result = await askWithRetry(async () => {
    throw new Error("network down");
  }, 1);

  // then
  assert.deepEqual(result, { status: "failed", message: "network down" });
});

test("should fill the entry fields the depositor left empty and name the file as their source", () => {
  // given: the depositor typed a space group already
  const metadata = { ...emptyEntryMetadataDraft(), spaceGroup: "P 1" };
  const extracted = {
    ...emptyExtractedFile(),
    resolution: 1.8,
    method: "X-RAY DIFFRACTION",
    spaceGroup: "P 21 21 21",
    ligands: [{ id: "ATP", reviewReason: null }],
    cofactors: [],
  };
  const file = depositFile();

  // when
  const { metadata: next, fill } = withEntryFileValues(metadata, file, extracted, "");
  const input = buildCreateEntryInput("Entry", next, null, [file]);
  const sources = entryValueSourcesRequest(input, [fill], [file]);

  // then
  assert.equal(next.resolution, "1.8");
  assert.equal(next.method, "X-ray crystallography");
  assert.equal(next.spaceGroup, "P 1");
  assert.deepEqual(next.compounds.ligands.items.map((item) => item.id), ["ATP"]);
  assert.deepEqual(sources, {
    "/metadata/ligands": { artifact_id: "deposit" },
    "/metadata/cofactors": { artifact_id: "deposit" },
    "/metadata/resolution": { artifact_id: "deposit" },
    "/metadata/method": { artifact_id: "deposit" },
  });
  assert.equal(entryFileBadges(input, [fill], [file]).resolution, "from deposit.cif");
});

test("should not name the file once the depositor changes its value or removes it", () => {
  // given
  const file = depositFile();
  const { metadata, fill } = withEntryFileValues(
    emptyEntryMetadataDraft(), file, { ...emptyExtractedFile(), resolution: 1.8 }, "",
  );
  const edited = { ...metadata, resolution: "1.9" };

  // when / then
  assert.equal(entryValueSourcesRequest(buildCreateEntryInput("Entry", edited, null, [file]), [fill], [file]), undefined);
  assert.equal(entryValueSourcesRequest(buildCreateEntryInput("Entry", metadata, null, []), [fill], []), undefined);
});

test("should fill typed mutations from the file only for an entity the depositor left empty", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    polymerEntities: [entity("1", []), entity("2", ["K7R"])],
  };
  const extracted = {
    ...emptyExtractedFile(),
    mutations: [
      { entityId: "1", accession: "P00698", mutations: ["C54T"], original: "C54T", needsReview: false },
      { entityId: "2", accession: "P00698", mutations: ["C97A"], original: "C97A", needsReview: false },
    ],
  };

  // when
  const fromDeposit = withEntryFileValues(metadata, depositFile(), extracted, "1ABC").metadata;
  const fromArchive = withEntryFileValues(metadata, depositFile(), { ...extracted, entryId: "1ABC" }, "1ABC").metadata;

  // then
  assert.deepEqual(fromDeposit.polymerEntities.map((item) => item.mutations), [["C54T"], ["K7R"]]);
  assert.deepEqual(fromArchive.polymerEntities.map((item) => item.mutations), [[], ["K7R"]]);
});

test("should note what the file says differently from the PDB and offer a PDB ID it names", () => {
  // given
  const read = [{ fileId: "deposit", fileName: "deposit.cif", extracted: { ...emptyExtractedFile(), entryId: "1XYZ", resolution: 2.1 } }];
  const archive = { locks: {}, title: null, metadata: {}, request: { resolution: 1.8 } };

  // when
  const messages = entryFileMessages(read, archive, "1ABC");

  // then
  assert.deepEqual(messages.map((message) => [message.field, message.message, message.blocking]), [
    ["entry.pdb", "the file deposit.cif names PDB 1XYZ, not 1ABC", false],
    ["entry.resolution", "the file deposit.cif gives 2.1; the form uses the PDB's value", false],
  ]);
  assert.deepEqual(pdbSuggestion(read, ""), { pdbId: "1XYZ", fileName: "deposit.cif" });
  assert.equal(pdbSuggestion(read, "1ABC"), null);
});

function depositFile() {
  return {
    id: "deposit",
    source: "upload",
    name: "deposit.cif",
    size: 10,
    type: "mmcif",
    artifactType: "other",
    level: "L0",
    authors: "",
    url: "https://files.example/deposit.cif",
    sha256: "a".repeat(64),
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
  };
}

function entity(labelEntityId, mutations) {
  return {
    labelEntityId,
    description: "",
    uniprotAccessions: ["P00698"],
    archiveOrganisms: ["Gallus gallus"],
    archiveConstruct: "",
    archiveMutations: [],
    archiveMutationsRaw: "",
    construct: "",
    mutations,
    organismName: "",
    organismTaxonomyId: "",
  };
}
