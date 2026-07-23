require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  authorsTextToList,
  buildCreateModelInput,
  canAddModelFiles,
  detectType,
  draftHasContent,
  modelFromDraft,
  modelValidationMessage,
  modelToDraft,
  fileEntityType,
  fileFromDraft,
  fileNameFromUrl,
  fileToDraft,
  formatSize,
  httpOnly,
  isPersistable,
  isValidHttpUrl,
  parseFasta,
  parseFile,
  parseUrlFile,
  programValidationMessage,
  sanitizeNumeric,
  toEntity,
  toMetricEntity,
  toProgramEntity,
  uploadStatusText,
  uploadsReady,
} = require("../src/app/components/NewEntryForm.tsx");

test("should convert files and metrics into create entry entities", () => {
  const modelFile = parsedFile({
    id: "model-1",
    name: "model.cif",
    type: "mmcif",
    level: "L2",
    size: 128,
    url: "s3://dynamic-pdb/model.cif",
    authors: " Alice ; Bob\nCarol ",
    affiliation: "  Example Lab  ",
    metadata: { pdb_id: "4HHB" },
  });
  const dataFile = parsedFile({
    id: "data-1",
    name: "sequence.fasta",
    type: "fasta",
    level: "L0",
    url: "s3://dynamic-pdb/sequence.fasta",
  });

  assert.equal(fileEntityType(modelFile), "model");
  assert.deepEqual(toEntity(modelFile), {
    id: "model-1",
    type: "model",
    level: "L2",
    name: "model.cif",
    payload: {
      file_url: "s3://dynamic-pdb/model.cif",
      size: 128,
      metadata: { pdb_id: "4HHB" },
      authors: ["Alice", "Bob", "Carol"],
      affiliation: "Example Lab",
    },
  });
  assert.deepEqual(toEntity(dataFile), {
    id: "data-1",
    type: "data",
    level: "L0",
    name: "sequence.fasta",
    payload: {
      file_url: "s3://dynamic-pdb/sequence.fasta",
      type: "fasta",
    },
  });
  assert.deepEqual(toMetricEntity({ id: "metrics-1", values: { r_free: "0.231", cc: "0.98", rscc: "" } }), {
    id: "metrics-1",
    type: "metrics",
    level: "L3",
    name: "Metrics",
    payload: { r_free: 0.231, cc: 0.98 },
  });
});

test("should build explicit graph relations for model metrics and program", () => {
  const modelFile = parsedFile({
    id: "model-entity-1",
    type: "mmcif",
    level: "L1",
    url: "s3://dynamic-pdb/model.cif",
  });
  const densityFile = parsedFile({
    id: "density-1",
    name: "density.ccp4",
    type: "ccp4",
    level: "L1",
    url: "s3://dynamic-pdb/density.ccp4",
  });
  const modelDraft = modelDraftFixture({
    id: "model-1",
    name: " Refined model ",
    description: " Description ",
    files: [modelFile, densityFile],
    metrics: [{ id: "metrics-1", values: { r_free: "0.231", cc: "0.98" } }],
    program: {
      id: "program-1",
      name: " phenix.refine ",
      version: " 1.21.2 ",
      description: " Refinement run ",
    },
  });

  const input = buildCreateModelInput(modelDraft);

  assert.equal(input.name, "Refined model");
  assert.equal(input.description, "Description");
  assert.deepEqual(input.entities.map((entity) => [entity.id, entity.type, entity.level]), [
    ["model-entity-1", "model", "L2"],
    ["density-1", "data", "L1"],
    ["metrics-1", "metrics", "L3"],
    ["program-1", "program", null],
  ]);
  assert.deepEqual(input.relations, [
    {
      source_entity_id: "metrics-1",
      target_entity_id: "model-entity-1",
      relation_type: "metrics_for",
    },
    {
      source_entity_id: "density-1",
      target_entity_id: "program-1",
      relation_type: "input_to",
    },
    {
      source_entity_id: "model-entity-1",
      target_entity_id: "program-1",
      relation_type: "output_of",
    },
  ]);
});

test("should validate the single model entity and complete program contract", () => {
  const modelFile = parsedFile({ id: "model-1", type: "mmcif" });
  const otherModelFile = parsedFile({ id: "model-2", type: "pdb" });
  const dataFile = parsedFile({ id: "data-1", type: "ccp4" });

  assert.equal(canAddModelFiles([modelFile], [dataFile]), true);
  assert.equal(canAddModelFiles([modelFile], [otherModelFile]), false);
  assert.equal(
    modelValidationMessage(modelDraftFixture({ files: [] })),
    "Add exactly one PDB/mmCIF model file.",
  );
  assert.equal(
    modelValidationMessage(modelDraftFixture({ files: [modelFile, otherModelFile] })),
    "Keep only one PDB/mmCIF model file.",
  );
  assert.equal(
    programValidationMessage({
      id: "program-1",
      name: "phenix.refine",
      version: "",
      description: "Refinement",
    }),
    "Fill program name, version, and description or remove the program.",
  );
  assert.deepEqual(
    toProgramEntity({
      id: "program-1",
      name: " phenix.refine ",
      version: " 1.21.2 ",
      description: " Refinement ",
    }),
    {
      id: "program-1",
      type: "program",
      level: null,
      name: "phenix.refine",
      payload: {
        name: "phenix.refine",
        version: "1.21.2",
        description: "Refinement",
      },
    },
  );
});

test("should normalize author and numeric inputs", () => {
  assert.deepEqual(authorsTextToList(" Alice ; Bob\n\n Carol "), ["Alice", "Bob", "Carol"]);
  assert.equal(sanitizeNumeric("abc-0,21.5x"), "-0.215");
  assert.equal(sanitizeNumeric("1.2.3"), "1.23");
  assert.equal(uploadStatusText("failed", "boom"), "Upload failed: boom");
});

test("should detect file types and parse fasta metadata", async () => {
  const fasta = ">chain A\nac gt\n>chain B\nTT\n";
  const file = new File([fasta], "sequence.fasta", { type: "text/plain" });

  const parsed = await parseFile(file, "L0");

  assert.equal(detectType("model.cif"), "mmcif");
  assert.equal(detectType("density.map"), "ccp4");
  assert.equal(detectType("notes.unknown"), "unknown");
  assert.equal(parsed.type, "fasta");
  assert.deepEqual(parsed.metadata, { chains: 2, length: 6, sequence: "ACGTTT" });
  assert.deepEqual(parseFasta("AC GT\n"), { chains: 1, length: 4, sequence: "ACGT" });
});

test("should parse URL files and skip unsafe URLs", async () => {
  const previousFetch = global.fetch;
  try {
    global.fetch = async () =>
      new Response(">chain A\nAAAA", {
        status: 200,
        headers: { "content-length": "13" },
      });

    assert.equal(await parseUrlFile("ftp://example.com/file.fasta", "L0"), null);
    const parsed = await parseUrlFile("https://example.com/path/sequence.fasta", "L0");

    assert.equal(fileNameFromUrl("https://example.com/path/model%201.cif"), "model 1.cif");
    assert.equal(isValidHttpUrl("https://example.com/model.cif"), true);
    assert.equal(isValidHttpUrl("ftp://example.com/model.cif"), false);
    assert.equal(parsed.name, "sequence.fasta");
    assert.equal(parsed.uploadStatus, "uploaded");
    assert.deepEqual(parsed.metadata, { chains: 1, length: 4, sequence: "AAAA" });
  } finally {
    global.fetch = previousFetch;
  }
});

test("should decide upload readiness from file and thumbnail states", () => {
  const uploaded = parsedFile({ id: "uploaded", url: "s3://dynamic-pdb/file.cif" });
  const pending = parsedFile({ id: "pending", url: "", uploadStatus: "uploading" });
  const model = {
    id: "model-1",
    name: "Model",
    description: "",
    thumbFileId: "thumb-1",
    thumbFile: null,
    thumbPreview: null,
    thumbUrl: null,
    thumbProgress: 0,
    thumbUploadStatus: "idle",
    thumbUploadError: null,
    files: [uploaded],
    metrics: [],
    program: null,
  };

  assert.equal(isPersistable(uploaded), true);
  assert.equal(isPersistable(pending), false);
  assert.equal(uploadsReady([uploaded], [model], null, null), true);
  assert.equal(uploadsReady([pending], [], null, null), false);
  assert.equal(uploadsReady([uploaded], [model], new File(["x"], "thumb.png"), null), false);
});

test("should serialize and restore persisted draft files safely", () => {
  const file = parsedFile({
    id: "file-1",
    url: "s3://dynamic-pdb/file.cif",
    preview: "blob:local-preview",
    metadata: { length: 42 },
  });
  const model = {
    id: "model-1",
    name: "Model",
    description: "Description",
    thumbFileId: "thumb-1",
    thumbFile: null,
    thumbPreview: "https://example.com/thumb.png",
    thumbUrl: "s3://dynamic-pdb/thumb.png",
    thumbProgress: 1,
    thumbUploadStatus: "uploaded",
    thumbUploadError: null,
    files: [file],
    metrics: [{ id: "metrics-1", values: { cc: "0.9" } }],
    program: {
      id: "program-1",
      name: "phenix.refine",
      version: "1.21.2",
      description: "Refinement",
    },
  };

  const storedFile = fileToDraft(file);
  const storedModel = modelToDraft(model);
  const restoredFile = fileFromDraft(storedFile);
  const restoredModel = modelFromDraft(storedModel);

  assert.equal(httpOnly("blob:local-preview"), undefined);
  assert.equal(httpOnly("https://example.com/thumb.png"), "https://example.com/thumb.png");
  assert.equal(storedFile.preview, undefined);
  assert.equal(restoredFile.uploadStatus, "uploaded");
  assert.equal(restoredFile.progress, 1);
  assert.equal(storedModel.thumbPreview, "https://example.com/thumb.png");
  assert.equal(restoredModel.thumbUploadStatus, "uploaded");
  assert.deepEqual(restoredModel.metrics, model.metrics);
  assert.deepEqual(restoredModel.program, model.program);
  assert.equal(
    draftHasContent({
      version: 1,
      entryId: "entry-1",
      name: "",
      description: "",
      thumbUrl: null,
      thumbUploadStatus: "idle",
      files: [],
      models: [],
    }),
    false,
  );
  assert.equal(
    draftHasContent({
      version: 1,
      entryId: "entry-1",
      name: "Draft",
      description: "",
      thumbUrl: null,
      thumbUploadStatus: "idle",
      files: [],
      models: [],
    }),
    true,
  );
});

test("should format byte sizes", () => {
  assert.equal(formatSize(0), "0 B");
  assert.equal(formatSize(512), "512 B");
  assert.equal(formatSize(1536), "1.5 KB");
  assert.equal(formatSize(2 * 1024 * 1024), "2 MB");
});

function parsedFile(overrides = {}) {
  return {
    id: "file-1",
    source: "url",
    name: "file.cif",
    size: 0,
    type: "mmcif",
    level: "L2",
    authors: "",
    affiliation: "",
    url: "s3://dynamic-pdb/file.cif",
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
    ...overrides,
  };
}

function modelDraftFixture(overrides = {}) {
  return {
    id: "model-1",
    name: "Model",
    description: "",
    thumbFileId: "thumb-1",
    thumbFile: null,
    thumbPreview: null,
    thumbUrl: null,
    thumbProgress: 0,
    thumbUploadStatus: "idle",
    thumbUploadError: null,
    files: [parsedFile({ id: "model-file-1", type: "mmcif" })],
    metrics: [],
    program: null,
    ...overrides,
  };
}
