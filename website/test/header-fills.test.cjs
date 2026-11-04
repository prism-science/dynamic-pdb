require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { detectFromFile } = require("../src/app/components/entry-form/autoDetect.ts");
const {
  liveHeaderFills,
  valueSourcesRequest,
  withoutStaleHeaderFills,
} = require("../src/app/components/entry-form/headerFills.ts");
const { emptyCompounds } = require("../src/app/components/entry-form/types.ts");
const { createModelRequestBody } = require("../src/lib/api/entries.ts");
const { buildCreateModelInput } = require("../src/app/components/entry-form/helpers.ts");

// The New model form fills R-factors and authors only from the model's own
// coordinate file, and names that file as their source in the request.

const HEADER = [
  "data_1ABC",
  "_entry.id 1ABC",
  "_audit_author.name 'Doe, J.'",
  "_refine.ls_R_factor_R_work 0.191",
  "_refine.ls_R_factor_R_free 0.232",
  "_software.name REFMAC",
  "",
].join("\n");

test("should fill scores and authors from the model file and remember the file", async () => {
  // given
  const modelFile = uploadedFile({ id: "model-file", name: "model.cif", artifactType: "model" });
  const draft = modelDraft({ files: [modelFile] });

  // when
  const patch = await detectFromFile(new File([HEADER], "model.cif"), modelFile, draft);

  // then
  assert.deepEqual(patch.metrics[0].values, { r_work: "0.191", r_free: "0.232" });
  assert.deepEqual(patch.authors, ["Doe, J."]);
  assert.deepEqual(patch.headerFills, {
    metrics: {
      r_work: { fileId: "model-file", fileName: "model.cif", value: "0.191" },
      r_free: { fileId: "model-file", fileName: "model.cif", value: "0.232" },
    },
    authors: { fileId: "model-file", fileName: "model.cif", value: ["Doe, J."] },
  });
});

test("should fill nothing of the model from a structure file that is not its model file", async () => {
  // given: the archive's mmCIF dropped as an input of a run
  const modelFile = uploadedFile({ id: "model-file", name: "model.pdb", type: "pdb", artifactType: "model" });
  const archiveFile = uploadedFile({ id: "archive-file", name: "1abc.cif", artifactType: "other" });
  const draft = modelDraft({ files: [modelFile, archiveFile] });

  // when
  const patch = await detectFromFile(new File([HEADER], "1abc.cif"), archiveFile, draft);

  // then
  assert.equal(patch.metrics, undefined);
  assert.equal(patch.authors, undefined);
  assert.equal(patch.programs, undefined);
  assert.equal(patch.compounds, undefined);
  assert.equal(patch.headerFills, undefined);
});

test("should name the model file as the source of each filled value the depositor left unchanged", () => {
  // given: R-free was edited after it was filled
  const draft = filledDraft({ metrics: [{ id: "m", values: { r_work: "0.191", r_free: "0.24" } }] });

  // when
  const sources = valueSourcesRequest(draft);
  const body = createModelRequestBody(buildCreateModelInput(draft));

  // then
  assert.deepEqual(sources, {
    "/metrics/r_work": { artifact_id: "model-file" },
    "/metadata/authors": { artifact_id: "model-file" },
  });
  assert.deepEqual(body.model.value_sources, sources);
  assert.equal(body.model.primary_artifact_id, "model-file");
});

test("should send no value sources when the model file has no checksum", () => {
  // given
  const draft = filledDraft({});
  draft.files = [{ ...draft.files[0], sha256: undefined }];

  // when / then
  assert.equal(valueSourcesRequest(draft), undefined);
});

test("should clear the unchanged values of a file that stops being the model file", () => {
  // given: R-free was edited, the rest is as filled
  const draft = filledDraft({ metrics: [{ id: "m", values: { r_work: "0.191", r_free: "0.24" } }] });
  draft.files = [{ ...draft.files[0], artifactType: "other" }];

  // when
  const next = withoutStaleHeaderFills(draft);

  // then
  assert.deepEqual(next.metrics[0].values, { r_free: "0.24" });
  assert.deepEqual(next.authors, []);
  assert.deepEqual(next.headerFills, { metrics: {}, authors: null });
  assert.equal(
    next.headerFillNotice,
    "Values read from model.cif were cleared: it is no longer the model file",
  );
  assert.deepEqual(liveHeaderFills(next), { metrics: {}, authors: null });
});

test("should leave a draft whose model file is unchanged as it is", () => {
  // given
  const draft = filledDraft({});

  // when / then
  assert.equal(withoutStaleHeaderFills(draft), draft);
  assert.equal(withoutStaleHeaderFills(modelDraft({})).headerFills, undefined);
});

function filledDraft(overrides) {
  return modelDraft({
    files: [uploadedFile({ id: "model-file", name: "model.cif", artifactType: "model", sha256: "a".repeat(64) })],
    metrics: [{ id: "m", values: { r_work: "0.191", r_free: "0.232" }, softwareName: "REFMAC" }],
    authors: ["Doe, J."],
    headerFills: {
      metrics: {
        r_work: { fileId: "model-file", fileName: "model.cif", value: "0.191" },
        r_free: { fileId: "model-file", fileName: "model.cif", value: "0.232" },
      },
      authors: { fileId: "model-file", fileName: "model.cif", value: ["Doe, J."] },
    },
    ...overrides,
  });
}

function modelDraft(overrides) {
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
    files: [],
    metrics: [],
    purpose: "",
    modelType: "",
    details: "",
    authors: [],
    affiliation: "",
    compounds: emptyCompounds(),
    compoundsSource: null,
    programs: [],
    ...overrides,
  };
}

function uploadedFile(overrides) {
  return {
    id: "file-1",
    source: "upload",
    name: "model.cif",
    size: 10,
    type: "mmcif",
    artifactType: "model",
    level: "L2",
    authors: "",
    url: "https://files.example/model.cif",
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
    ...overrides,
  };
}
