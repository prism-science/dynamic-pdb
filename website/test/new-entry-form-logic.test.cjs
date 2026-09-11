require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  assignCanonicalArtifactTypes,
  authorsTextToList,
  buildCreateModelInput,
  canAddModelFiles,
  detectType,
  draftHasContent,
  extFileKeys,
  extFileToParsed,
  modelValidationMessage,
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
  programsValidationMessage,
  sanitizeNumeric,
  toEntity,
  toMetricEntity,
  toProgramEntity,
  uploadStatusText,
  uploadsReady,
} = require("../src/app/components/NewEntryForm.tsx");
const { fastaRecordText, fastaTotalLength } = require("../src/lib/fasta.ts");

test("should convert files and metrics into create entry entities", () => {
  const modelFile = parsedFile({
    id: "model-1",
    name: "model.cif",
    type: "mmcif",
    level: "L2",
    size: 128,
    url: "s3://dynamic-pdb/model.cif",
    authors: " Alice ; Bob\nCarol ",
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
    artifact_type: "model",
    level: "L2",
    name: "model.cif",
    payload: {
      file_url: "s3://dynamic-pdb/model.cif",
      size: 128,
      metadata: { pdb_id: "4HHB" },
      authors: ["Alice", "Bob", "Carol"],
    },
  });
  assert.deepEqual(toEntity(dataFile), {
    id: "data-1",
    type: "data",
    artifact_type: "fasta",
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
    payload: { r_free: 0.231 },
  });
});

test("should assign only the first canonical file for each scope", () => {
  // given
  const fastaFiles = [
    parsedFile({ id: "fasta-1", type: "fasta", artifactType: "other" }),
    parsedFile({ id: "fasta-2", type: "fasta", artifactType: "other" }),
  ];
  const coordinateFiles = [
    parsedFile({ id: "model-1", artifactType: "other" }),
    parsedFile({ id: "auxiliary-1", artifactType: "other" }),
  ];

  // when
  const entryFiles = assignCanonicalArtifactTypes([], fastaFiles, "entry");
  const modelFiles = assignCanonicalArtifactTypes([], coordinateFiles, "model");

  // then
  assert.deepEqual(
    entryFiles.map((file) => file.artifactType),
    ["fasta", "other"],
  );
  assert.deepEqual(
    modelFiles.map((file) => file.artifactType),
    ["model", "other"],
  );
});

test("should preserve Ext provenance when linked file becomes an entity", () => {
  const experimentId = "123e4567-e89b-42d3-a456-426614174003";
  const linked = extFileToParsed(
    {
      name: "model.cif",
      path: "/mnt/diffuse-shared/model.cif",
      sha256: "sha-123",
      size: 42,
      directory: false,
      available: true,
      metadata: { detector: "eiger" },
      directions: ["output"],
    },
    experimentId,
    "L2",
  );

  const entity = toEntity(linked);
  const restored = fileFromDraft(fileToDraft(linked));

  assert.equal(linked.source, "ext");
  assert.equal(linked.uploadStatus, "uploaded");
  assert.equal(entity.payload.sha256, "sha-123");
  assert.match(linked.url, new RegExp(`^ext://${experimentId}\\?`));
  assert.deepEqual(entity.payload.metadata, {
    detector: "eiger",
    _dynamic_pdb_source: {
      provider: "ext",
      experiment_id: experimentId,
      path: "/mnt/diffuse-shared/model.cif",
      sha256: "sha-123",
    },
  });
  assert.deepEqual(restored.extReference, linked.extReference);
  assert.equal(restored.sha256, "sha-123");
  assert.deepEqual(Array.from(extFileKeys([restored])), [
    "/mnt/diffuse-shared/model.cif\u0000sha-123",
  ]);
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
    title: " Refined model ",
    files: [modelFile, densityFile],
    metrics: [{ id: "metrics-1", values: { r_free: "0.231" } }],
    programs: [
      {
        id: "program-1",
        name: " phenix.refine ",
        version: " 1.21.2 ",
        description: " Refinement run ",
        inputFileIds: ["density-1"],
        outputFileIds: ["model-entity-1"],
        expectedInputs: [],
        origin: "manual",
      },
    ],
  });

  const input = buildCreateModelInput(modelDraft);

  assert.equal(input.title, "Refined model");
  assert.deepEqual(input.metadata, {});
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

test("should record existing entry entities as program inputs", () => {
  const modelDraft = modelDraftFixture({
    files: [parsedFile({ id: "model-entity-1", type: "mmcif" })],
    programs: [
      {
        id: "program-1",
        name: "phenix.refine",
        version: "1.21.2",
        description: "Refinement",
        inputFileIds: ["existing-l0-1", "existing-l0-2"],
        outputFileIds: [],
        expectedInputs: [],
        origin: "manual",
      },
    ],
  });

  const input = buildCreateModelInput(modelDraft, {
    programInputEntityIds: ["existing-l0-1", "existing-l0-2"],
  });

  assert.deepEqual(
    input.relations.filter((relation) => relation.relation_type === "input_to"),
    [
      {
        source_entity_id: "existing-l0-1",
        target_entity_id: "program-1",
        relation_type: "input_to",
      },
      {
        source_entity_id: "existing-l0-2",
        target_entity_id: "program-1",
        relation_type: "input_to",
      },
    ],
  );

  // Without a program there is nothing for the inputs to point at.
  const withoutProgram = buildCreateModelInput(
    modelDraftFixture({ programs: [] }),
    { programInputEntityIds: ["existing-l0-1"] },
  );
  assert.deepEqual(withoutProgram.relations, []);
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
  // A run with no name cannot be recorded; a missing version is simply a
  // version nobody wrote down.
  assert.equal(
    programsValidationMessage([
      {
        id: "program-1",
        name: "  ",
        version: "1.21.2",
        description: "Refinement",
        inputFileIds: [],
        outputFileIds: [],
        expectedInputs: [],
        origin: "manual",
      },
    ]),
    "Give every program a name or remove it.",
  );
  assert.equal(
    programsValidationMessage([
      {
        id: "program-1",
        name: "phenix.refine",
        version: "",
        description: "",
        inputFileIds: [],
        outputFileIds: [],
        expectedInputs: [],
        origin: "manual",
      },
    ]),
    null,
  );
  assert.deepEqual(
    toProgramEntity({
      id: "program-1",
      name: " phenix.refine ",
      version: " 1.21.2 ",
      description: " Refinement ",
      inputFileIds: [],
      outputFileIds: [],
      expectedInputs: [],
      origin: "manual",
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
  const fasta =
    ">4HHB_1|Chains A,C|Hemoglobin subunit alpha|Homo sapiens\nac gt\n" +
    ">4HHB_2|Chains B,D|Hemoglobin subunit beta|Homo sapiens\nTT\n";
  const file = new File([fasta], "sequence.fasta", { type: "text/plain" });

  const parsed = await parseFile(file, "L0");

  assert.equal(detectType("model.cif"), "mmcif");
  assert.equal(detectType("density.map"), "ccp4");
  assert.equal(detectType("notes.unknown"), "unknown");
  assert.equal(parsed.type, "fasta");
  assert.deepEqual(parsed.metadata, {
    records: [
      {
        header: "4HHB_1|Chains A,C|Hemoglobin subunit alpha|Homo sapiens",
        sequence: "ACGT",
      },
      {
        header: "4HHB_2|Chains B,D|Hemoglobin subunit beta|Homo sapiens",
        sequence: "TT",
      },
    ],
  });
  assert.deepEqual(parseFasta("AC GT\n"), {
    records: [{ header: "", sequence: "ACGT" }],
  });
});

test("should read record-based fasta metadata", () => {
  const metadata = parseFasta(
    ">4HHB_1|Chains A,C|Hemoglobin alpha\nACGT\n" +
      ">4HHB_2|Chains B,D|Hemoglobin beta\nTT\n",
  );

  assert.equal(fastaTotalLength(metadata), 6);
  assert.equal(
    fastaRecordText(metadata.records[0]),
    ">4HHB_1|Chains A,C|Hemoglobin alpha\nACGT\n",
  );
});

test("should wrap a written-out fasta record at 60 columns", () => {
  const sequence = "A".repeat(130);
  assert.equal(
    fastaRecordText({ header: "", sequence }, "Sequence"),
    ">Sequence\n" + "A".repeat(60) + "\n" + "A".repeat(60) + "\n" + "A".repeat(10) + "\n",
  );
});

test("should parse URL files and skip unsafe URLs", async () => {
  const previousFetch = global.fetch;
  try {
    global.fetch = async () =>
      new Response(">example protein\nAAAA", {
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
    assert.deepEqual(parsed.metadata, {
      records: [{ header: "example protein", sequence: "AAAA" }],
    });
  } finally {
    global.fetch = previousFetch;
  }
});

test("should decide upload readiness from file and thumbnail states", () => {
  const uploaded = parsedFile({ id: "uploaded", url: "s3://dynamic-pdb/file.cif" });
  const pending = parsedFile({ id: "pending", url: "", uploadStatus: "uploading" });

  assert.equal(isPersistable(uploaded), true);
  assert.equal(isPersistable(pending), false);
  assert.equal(uploadsReady([uploaded], null, null), true);
  assert.equal(uploadsReady([pending], null, null), false);
  assert.equal(uploadsReady([uploaded], new File(["x"], "thumb.png"), null), false);
});

test("should serialize and restore persisted draft files safely", () => {
  const file = parsedFile({
    id: "file-1",
    url: "s3://dynamic-pdb/file.cif",
    preview: "blob:local-preview",
    metadata: { length: 42 },
  });

  const storedFile = fileToDraft(file);
  const restoredFile = fileFromDraft(storedFile);

  assert.equal(httpOnly("blob:local-preview"), undefined);
  assert.equal(httpOnly("https://example.com/thumb.png"), "https://example.com/thumb.png");
  // A blob: preview is local to the tab that made it, so it is dropped rather
  // than restored as a broken image.
  assert.equal(storedFile.preview, undefined);
  assert.equal(restoredFile.uploadStatus, "uploaded");
  assert.equal(restoredFile.progress, 1);
  assert.deepEqual(restoredFile.metadata, { length: 42 });

  assert.equal(
    draftHasContent({
      version: 1,
      entryId: "entry-1",
      title: "",
      thumbUrl: null,
      thumbUploadStatus: "idle",
      files: [],
    }),
    false,
  );
  assert.equal(
    draftHasContent({
      version: 1,
      entryId: "entry-1",
      title: "Draft",
      thumbUrl: null,
      thumbUploadStatus: "idle",
      files: [],
    }),
    true,
  );
  assert.equal(
    draftHasContent({
      version: 1,
      entryId: "entry-1",
      title: "",
      thumbUrl: null,
      thumbUploadStatus: "idle",
      files: [storedFile],
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
  const fileType = overrides.type ?? "mmcif";
  const artifactType =
    overrides.artifactType ??
    (fileType === "pdb" || fileType === "mmcif"
      ? "model"
      : fileType === "fasta"
        ? "fasta"
        : "other");
  return {
    id: "file-1",
    source: "url",
    name: "file.cif",
    size: 0,
    type: fileType,
    artifactType,
    level: "L2",
    authors: "",
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
    title: "Model",
    thumbFileId: "thumb-1",
    thumbFile: null,
    thumbPreview: null,
    thumbUrl: null,
    thumbProgress: 0,
    thumbUploadStatus: "idle",
    thumbUploadError: null,
    files: [parsedFile({ id: "model-file-1", type: "mmcif" })],
    metrics: [],
    purpose: "",
    modelType: "",
    programs: [],
    ...overrides,
  };
}

test("purpose and model type travel with the model request", () => {
  const input = buildCreateModelInput(
    modelDraftFixture({
      purpose: "Refinement",
      modelType: "Multiconformer",
    }),
  );

  assert.deepEqual(input.metadata, {
    purpose: "Refinement",
    model_type: "Multiconformer",
  });
});
