require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

// The *Values arrays are written by `npm run gen:api` from backend/api/openapi.yaml,
// and CI fails when they differ from a fresh run, so comparing a dropdown with them
// compares it with the API's enum.
const {
  artifactLevelValues,
  artifactTypeValues,
  metricKeyValues,
  modelPurposeValues,
  structureMethodValues,
  structureModelTypeValues,
} = require("../src/lib/api/schema.gen.ts");
const {
  emptyEntryMetadataDraft,
  LEVELS,
  METHODS,
  METRIC_FIELDS,
  MODEL_FILE_TYPES,
  MODEL_PURPOSES,
  MODEL_TYPES,
} = require("../src/app/components/entry-form/types.ts");
const FilesEditor = require("../src/app/components/entry-form/FilesEditor.tsx").default;
const EntryFormSections = require("../src/app/components/entry-form/EntryFormSections.tsx").default;
const {
  archiveFieldValues,
  archiveFieldsView,
  archiveValuesFromAutofill,
} = require("../src/app/components/entry-form/archiveChoices.ts");

/** The values of the options of the select whose attribute is value in html. */
function selectOptions(html, value, attribute = "aria-label") {
  const select = html.match(new RegExp(`<select[^>]*${attribute}="${value}"[^>]*>(.*?)</select>`));
  assert.ok(select, `no select with ${attribute}="${value}"`);
  return [...select[1].matchAll(/<option[^>]*value="([^"]*)"/g)].map((match) => match[1]);
}

/** The New entry form's sections, with no PDB ID looked up. */
function renderEntrySections(metadata) {
  return renderToStaticMarkup(
    React.createElement(EntryFormSections, {
      title: "Lysozyme",
      onTitle: () => {},
      thumbnail: {
        file: null, preview: null, url: null, progress: 0, status: "idle", error: null,
        choose: () => {}, restore: () => {}, reset: () => {},
      },
      metadata,
      onEdit: () => {},
      onPdb: () => {},
      lookup: { status: "idle" },
      onRetryLookup: () => {},
      archiveFields: archiveFieldsView(
        {},
        archiveValuesFromAutofill(null),
        archiveFieldValues({ title: "Lysozyme", metadata }),
        [],
        true,
      ),
      onOwn: () => {},
      messages: [],
      data: null,
    }),
  );
}

function renderModelFiles(files) {
  return renderToStaticMarkup(
    React.createElement(FilesEditor, {
      files,
      onAdd: () => {},
      onAddUrl: () => {},
      onRemove: () => {},
      onLevel: () => {},
      onPatch: () => {},
      fileMessages: () => [],
      artifactTypeOptions: MODEL_FILE_TYPES,
    }),
  );
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

test("should_offer_every_method_the_api_accepts_from_a_depositor", () => {
  // given
  const metadata = emptyEntryMetadataDraft();

  // when
  const html = renderEntrySections(metadata);

  // then
  assert.deepEqual(selectOptions(html, "entry-method", "id"), ["", ...structureMethodValues]);
  assert.deepEqual([...METHODS], ["X-ray crystallography", "CryoEM"]);
});

test("should_keep_showing_a_method_off_the_list_when_a_restored_draft_has_one", () => {
  // given
  const metadata = { ...emptyEntryMetadataDraft(), method: "X-ray diffraction" };

  // when
  const html = renderEntrySections(metadata);

  // then
  assert.deepEqual(selectOptions(html, "entry-method", "id"), ["", "X-ray diffraction", ...structureMethodValues]);
  assert.ok(html.includes("X-ray diffraction (not on the list)"));
});

test("should_offer_every_artifact_level_in_the_level_dropdown", () => {
  // given
  const files = [uploadedFile({ id: "data", name: "data.mtz", type: "mtz", artifactType: "other", level: "L0" })];

  // when
  const html = renderModelFiles(files);

  // then
  assert.deepEqual(selectOptions(html, "Level"), [...artifactLevelValues]);
  assert.deepEqual([...LEVELS], [...artifactLevelValues]);
});

test("should_offer_the_artifact_types_except_model_and_fasta_for_a_models_other_files", () => {
  // given
  const files = [
    uploadedFile({ id: "model", name: "model.cif" }),
    uploadedFile({ id: "data", name: "data.mtz", type: "mtz", artifactType: "structure_factors", level: "L0" }),
  ];

  // when
  const html = renderModelFiles(files);
  const [, modelFile, dataFile] = html.split("<li");

  // then
  const expected = artifactTypeValues.filter((type) => type !== "model" && type !== "fasta");
  assert.deepEqual(expected, ["structure_factors", "other"]);
  assert.deepEqual(selectOptions(dataFile, "File type"), expected);
  assert.deepEqual(selectOptions(modelFile, "File type"), ["model"], "the model file keeps its own type");
});

test("should_offer_every_model_purpose_except_reference", () => {
  // given / when / then
  assert.deepEqual(MODEL_PURPOSES, modelPurposeValues.filter((purpose) => purpose !== "Reference"));
});

test("should_offer_every_model_type_except_deposited", () => {
  // given / when / then
  assert.deepEqual(MODEL_TYPES, structureModelTypeValues.filter((type) => type !== "Deposited"));
});

test("should_have_a_metric_field_for_every_metric_key", () => {
  // given / when
  const keys = METRIC_FIELDS.map((field) => field.key);

  // then
  assert.deepEqual([...keys].sort(), [...metricKeyValues].sort());
});
