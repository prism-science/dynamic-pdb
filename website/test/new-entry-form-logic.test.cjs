require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  authorsTextToList,
  detectType,
  draftHasContent,
  modelFromDraft,
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
  sanitizeNumeric,
  toEntity,
  toMetricEntity,
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
