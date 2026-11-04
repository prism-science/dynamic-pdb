require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

const entries = require("../src/lib/api/entries.ts");
const { fieldForPointer, placeApiErrors } = require("../src/app/components/entry-form/apiErrors.ts");
const { buildCreateModelInput, emptyProgramDraft } = require("../src/app/components/entry-form/helpers.ts");
const { blockerLines } = require("../src/app/components/entry-form/messages.ts");
const {
  buildModelRevisionChange,
  modelDraftFromModel,
  modelLocks,
} = require("../src/app/components/entry-form/revisionChanges.ts");
const { emptyCompounds } = require("../src/app/components/entry-form/types.ts");
const {
  editedModelSoftwareRule,
  isComputedModelType,
  modelEditMessages,
  modelFormMessages,
  newModelSoftwareRule,
} = require("../src/app/components/entry-form/validation.ts");
const MetricsEditor = require("../src/app/components/entry-form/MetricsEditor.tsx").default;

// T1-E: the add-model and edit forms ask for what the API's provenance rules
// require of a computed model (backend/internal/validation/provenance.go), so
// nothing they send gets a 422 once validation.mode is enforce.

// ------------------------------------------------------- computed models --

test("should_treat_every_model_type_but_deposited_as_computed", () => {
  // given
  const cases = [
    ["", true],
    ["Single Conformer", true],
    ["Multiconformer", true],
    ["Ensemble", true],
    ["not a type", true],
    ["Deposited", false],
    [" Deposited ", false],
  ];

  // when / then
  for (const [modelType, computed] of cases) {
    assert.equal(isComputedModelType(modelType), computed, modelType);
  }
});

// -------------------------------------------------------------- programs --

test("should_require_a_program_when_the_model_has_none", () => {
  // given
  const draft = modelDraft({ programs: [] });

  // when
  const messages = programMessagesOf(draft);

  // then
  assert.deepEqual(messages, [["model.runs", null, "add the program that made this model"]]);
  assert.deepEqual(blockerLines(modelFormMessages(draft)), [
    "Programs: add the program that made this model",
  ]);
});

test("should_require_version_input_and_output_of_each_program", () => {
  // given
  const draft = modelDraft({
    programs: [{ ...emptyProgramDraft({ id: "p-1" }), name: "phenix.refine" }],
  });

  // when
  const messages = programMessagesOf(draft);

  // then
  assert.deepEqual(messages, [
    ["run.version", "p-1", "required"],
    ["run.files", "p-1", "choose at least one input file"],
    ["run.files", "p-1", "choose at least one output file of this model"],
  ]);
  assert.deepEqual(blockerLines(modelFormMessages(draft)), [
    "Version (step 1): required",
    "Inputs and outputs (step 1): choose at least one input file",
    "Inputs and outputs (step 1): choose at least one output file of this model",
  ]);
});

test("should_accept_a_complete_program_reading_a_file_of_the_entry", () => {
  // given
  const draft = modelDraft({
    programs: [completeProgram({ inputFileIds: ["entry-file-1"] })],
  });

  // when
  const messages = modelFormMessages(draft, new Set(["entry-file-1"]));

  // then
  assert.deepEqual(messages, []);
});

test("should_count_only_links_to_files_the_request_carries", () => {
  // given: an input removed from the draft, and an output that is an entry
  // file, which the API never takes as an output
  const draft = modelDraft({
    programs: [
      completeProgram({ inputFileIds: ["removed-file"], outputFileIds: ["entry-file-1"] }),
    ],
  });

  // when
  const messages = programMessagesOf(draft, new Set(["entry-file-1"]));

  // then
  assert.deepEqual(messages, [
    ["run.files", "p-1", "choose at least one input file"],
    ["run.files", "p-1", "choose at least one output file of this model"],
  ]);
});

test("should_not_ask_a_deposited_model_for_programs_or_score_software", () => {
  // given
  const draft = modelDraft({
    modelType: "Deposited",
    programs: [],
    metrics: [{ id: "m-1", values: { r_work: "0.2" } }],
  });

  // when
  const messages = modelFormMessages(draft);

  // then: only the hidden-choice check, which a depositor cannot pass anyway
  assert.deepEqual(messages.map((message) => message.field), ["model.type"]);
});

// ---------------------------------------------------------- score software --

test("should_require_score_software_unless_a_program_step_computed_the_scores", () => {
  // given
  const program = completeProgram();
  const unnamed = modelDraft({
    programs: [program],
    metrics: [{ id: "m-1", values: { r_work: "0.2" } }],
  });
  const named = modelDraft({
    programs: [program],
    metrics: [{ id: "m-1", values: { r_work: "0.2" }, softwareName: "MolProbity" }],
  });
  const linked = modelDraft({
    programs: [program],
    metrics: [{ id: "m-1", values: { r_work: "0.2" }, runId: program.id }],
  });
  const linkedToRemovedProgram = modelDraft({
    programs: [program],
    metrics: [{ id: "m-1", values: { r_work: "0.2" }, runId: "removed-program" }],
  });
  const empty = modelDraft({ programs: [program], metrics: [{ id: "m-1", values: {} }] });

  // when / then
  assert.deepEqual(blockerLines(modelFormMessages(unnamed)), [
    "Software: name the software that computed the scores, or choose the program step that did",
  ]);
  assert.deepEqual(modelFormMessages(named), []);
  assert.deepEqual(modelFormMessages(linked), []);
  assert.equal(modelFormMessages(linkedToRemovedProgram).length, 1);
  assert.deepEqual(modelFormMessages(empty), []);
});

test("should_send_the_program_as_run_id_and_no_software_when_scores_are_linked", async () => {
  // given: software typed before the scores were linked stays out
  const program = completeProgram();
  const draft = modelDraft({
    programs: [program],
    metrics: [
      { id: "m-1", values: { r_work: "0.2", r_free: "0.25" }, softwareName: "MolProbity", runId: program.id },
    ],
  });

  // when
  const body = await sentModelBody(buildCreateModelInput(draft));

  // then
  assert.deepEqual(
    body.model.metrics.map(({ key, run_id, software_name }) => ({ key, run_id, software_name })),
    [
      { key: "r_work", run_id: program.id, software_name: undefined },
      { key: "r_free", run_id: program.id, software_name: undefined },
    ],
  );
  assert.deepEqual(body.model.runs.map((run) => run.id), [program.id]);
});

test("should_send_the_named_software_and_no_run_id_when_the_linked_program_was_removed", async () => {
  // given
  const draft = modelDraft({
    programs: [completeProgram()],
    metrics: [
      { id: "m-1", values: { r_work: "0.2" }, softwareName: "MolProbity", softwareVersion: "4.5.2", runId: "removed" },
    ],
  });

  // when
  const body = await sentModelBody(buildCreateModelInput(draft));

  // then
  assert.deepEqual(body.model.metrics.map(({ key, run_id, software_name, software_version }) => ({
    key,
    run_id,
    software_name,
    software_version,
  })), [{ key: "r_work", run_id: undefined, software_name: "MolProbity", software_version: "4.5.2" }]);
});

test("should_offer_the_programs_as_what_computed_the_scores_without_choosing_one", () => {
  // given
  const draft = modelDraft({
    programs: [completeProgram(), { ...emptyProgramDraft({ id: "p-2" }) }],
    metrics: [{ id: "m-1", values: { r_work: "0.2" } }],
  });

  // when
  const html = renderMetricsEditor(draft.metrics, draft.programs, newModelSoftwareRule(draft));

  // then
  assert.match(html, /Computed by/);
  assert.match(html, /<option value="" selected="">Software named here<\/option>/);
  assert.match(html, /<option value="p-1">Step 1: phenix.refine 1.21.2<\/option>/);
  assert.match(html, /<option value="p-2">Step 2: unnamed program<\/option>/);
  assert.match(html, /Software \(required\)/);
});

test("should_hide_the_software_fields_when_a_program_step_computed_the_scores", () => {
  // given
  const program = completeProgram();
  const draft = modelDraft({
    programs: [program],
    metrics: [{ id: "m-1", values: { r_work: "0.2" }, runId: program.id }],
  });

  // when
  const html = renderMetricsEditor(draft.metrics, draft.programs, newModelSoftwareRule(draft));

  // then
  assert.match(html, /<option value="p-1" selected="">Step 1: phenix.refine 1.21.2<\/option>/);
  assert.doesNotMatch(html, /Software version/);
  assert.doesNotMatch(html, /placeholder="e.g. MolProbity"/);
});

// --------------------------------------------------------------- edit form --

test("should_require_score_software_when_an_edit_sends_the_scores_of_a_computed_model", () => {
  // given: a computed model whose stored scores name no software
  const model = storedModel({
    metadata: { model_type: "Multiconformer" },
    metrics: [storedMetric("r_work", 0.2)],
  });
  const initial = modelDraftFromModel(model);
  const edited = withScore(initial, "r_work", "0.19");
  const change = buildModelRevisionChange(model, initial, edited);
  const sent = { changed: change.metrics !== undefined, storedRunIds: new Set() };

  // when
  const messages = modelEditMessages(edited, noLocks(), sent);

  // then
  assert.equal(sent.changed, true);
  assert.deepEqual(blockerLines(messages), [
    "Software: name the software that computed the scores, or choose the program step that did",
  ]);
  assert.equal(editedModelSoftwareRule(edited, sent)(edited.metrics[0]), true);
  assert.match(
    renderMetricsEditor(edited.metrics, undefined, editedModelSoftwareRule(edited, sent)),
    /Software \(required\)/,
  );
});

test("should_not_require_score_software_when_the_edit_leaves_the_scores_alone", () => {
  // given
  const model = storedModel({ metrics: [storedMetric("r_work", 0.2)] });
  const initial = modelDraftFromModel(model);
  const edited = { ...initial, title: "Renamed" };
  const change = buildModelRevisionChange(model, initial, edited);
  const sent = { changed: change.metrics !== undefined, storedRunIds: new Set() };

  // when
  const messages = modelEditMessages(edited, noLocks(), sent);

  // then
  assert.equal(sent.changed, false);
  assert.deepEqual(messages, []);
  assert.doesNotMatch(
    renderMetricsEditor(edited.metrics, undefined, editedModelSoftwareRule(edited, sent)),
    /Software \(required\)/,
  );
});

test("should_keep_the_stored_run_of_edited_scores_and_need_no_software", () => {
  // given
  const model = storedModel({ metrics: [storedMetric("r_work", 0.2, { run_id: "run-1" })] });
  const initial = modelDraftFromModel(model);
  const edited = withScore(initial, "r_work", "0.19");
  const change = buildModelRevisionChange(model, initial, edited, () => "new-id");
  const sent = { changed: true, storedRunIds: new Set(["run-1"]) };

  // when
  const messages = modelEditMessages(edited, noLocks(), sent);

  // then
  assert.deepEqual(messages, []);
  assert.deepEqual(change.metrics, [
    { key: "r_work", value: 0.19, software_name: null, software_version: null, run_id: "run-1", id: "new-id" },
  ]);
});

test("should_not_require_score_software_of_a_deposited_model", () => {
  // given
  const model = storedModel({
    metadata: { model_type: "Deposited" },
    metrics: [storedMetric("rscc", 0.9)],
  });
  const initial = modelDraftFromModel(model);
  const edited = withScore(initial, "rscc", "0.95");

  // when
  const messages = modelEditMessages(edited, modelLocks(model), { changed: true, storedRunIds: new Set() });

  // then
  assert.deepEqual(messages, []);
});

// ------------------------------------------------------------ API errors --

test("should_place_the_api_provenance_errors_on_the_programs_and_scores", () => {
  // given
  const request = {
    model: {
      metrics: [{ id: "metric-1", key: "r_work", value: 0.2 }],
      runs: [{ id: "run-1", name: "phenix.refine" }],
    },
  };
  const errors = [
    { status: "422", detail: "a computed model needs at least one run", source: { pointer: "/model/runs" } },
    { status: "422", detail: "needs at least one input artifact", source: { pointer: "/model/runs/0/artifacts" } },
    { status: "422", detail: "is not allowed", source: { pointer: "/model/runs/0/metadata/description" } },
    { status: "422", detail: "is required", source: { pointer: "/model/metrics/0/software_name" } },
  ];

  // when
  const { placed, unplaced } = placeApiErrors(errors, request);

  // then
  assert.equal(fieldForPointer("/model/runs"), "model.runs");
  assert.deepEqual(
    placed.map(({ field, item }) => [field, item]),
    [
      ["model.runs", null],
      ["run.files", "run-1"],
      ["run.description", "run-1"],
      ["model.metrics.software", "r_work"],
    ],
  );
  assert.deepEqual(unplaced, []);
});

// ---------------------------------------------------------- request shape --

test("should_send_the_run_description_under_metadata_extensions", async () => {
  // given
  const draft = modelDraft({ programs: [completeProgram({ description: " Refinement " })] });

  // when
  const body = await sentModelBody(buildCreateModelInput(draft));

  // then
  assert.deepEqual(body.model.runs[0].metadata, { extensions: { description: "Refinement" } });
});

test("should_send_empty_run_metadata_when_the_program_has_no_description", async () => {
  // given
  const draft = modelDraft({ programs: [completeProgram()] });

  // when
  const body = await sentModelBody(buildCreateModelInput(draft));

  // then
  assert.deepEqual(body.model.runs[0].metadata, {});
});

test("should_read_the_run_description_from_its_new_and_its_old_place", async () => {
  // given
  const runs = [
    storedRun("run-new", { extensions: { description: "Refined with qFit" } }),
    storedRun("run-old", { description: "Refinement" }),
    storedRun("run-none", {}),
  ];

  // when
  const data = await withFetch(modelPageFetch(runs), () =>
    entries.getModelPageData("token", "entry-1", "model-1"),
  );

  // then
  const programs = data.entities.filter((entity) => entity.type === "program");
  assert.deepEqual(
    programs.map((program) => [program.id, program.payload.description]),
    [
      ["run-new", "Refined with qFit"],
      ["run-old", "Refinement"],
      ["run-none", ""],
    ],
  );
});

test("should_send_an_explicit_format_and_a_checksum_in_lowercase", async () => {
  // given: an Ext file whose checksum Ext reports in uppercase
  const sha256 = "AB".repeat(32);
  const input = {
    title: "Model",
    entities: [
      {
        id: "file-1",
        type: "data",
        artifact_type: "structure_factors",
        level: "L1",
        name: "data.MTZ",
        payload: {
          file_url: "https://files.example/data.MTZ",
          sha256,
          type: "MTZ",
          metadata: { _dynamic_pdb_source: { provider: "ext", path: "data.MTZ", sha256 } },
        },
      },
    ],
  };

  // when
  const body = await sentModelBody(input);

  // then
  const [artifact] = body.model.artifacts;
  assert.equal(artifact.format, "mtz");
  assert.equal(artifact.sha256, "ab".repeat(32));
  // The Ext reference stays as Ext gave it: it is how the file is found there.
  assert.equal(artifact.metadata._dynamic_pdb_source.sha256, sha256);
});

// --------------------------------------------------------------- helpers --

function programMessagesOf(draft, baselineFileIds) {
  return modelFormMessages(draft, baselineFileIds)
    .filter((message) => message.field === "model.runs" || message.field.startsWith("run."))
    .map((message) => [message.field, message.item, message.message]);
}

function completeProgram(overrides = {}) {
  return {
    ...emptyProgramDraft({ id: "p-1" }),
    name: "phenix.refine",
    version: "1.21.2",
    inputFileIds: ["data-file-1"],
    outputFileIds: ["model-file-1"],
    ...overrides,
  };
}

function modelDraft(overrides = {}) {
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
    files: [
      draftFile({ id: "model-file-1", name: "model.cif", type: "mmcif", artifactType: "model", level: "L2" }),
      draftFile({ id: "data-file-1", name: "data.mtz", type: "mtz", artifactType: "structure_factors", level: "L1" }),
    ],
    metrics: [],
    purpose: "",
    modelType: "",
    details: "",
    authors: [],
    affiliation: "",
    compounds: emptyCompounds(),
    compoundsSource: null,
    programs: [completeProgram()],
    ...overrides,
  };
}

function draftFile(overrides) {
  return {
    source: "url",
    size: 10,
    authors: "",
    url: `https://files.example/${overrides.name}`,
    progress: 1,
    uploadStatus: "uploaded",
    uploadError: null,
    ...overrides,
  };
}

function storedModel(overrides = {}) {
  return {
    id: "model-1",
    entry_id: "entry-1",
    title: "Model",
    thumbnail_image_url: null,
    primary_artifact_id: null,
    metadata: {},
    metrics: [],
    field_sources: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function storedMetric(key, value, overrides = {}) {
  return { id: `metric-${key}`, key, value, created_at: "2026-01-01T00:00:00Z", ...overrides };
}

function storedRun(id, metadata) {
  return {
    id,
    name: "phenix.refine",
    software_name: "phenix.refine",
    software_version: "1.21.2",
    command: null,
    parameters: {},
    metadata,
    created_by: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function withScore(draft, key, value) {
  return {
    ...draft,
    metrics: draft.metrics.map((metric, index) =>
      index === 0 ? { ...metric, values: { ...metric.values, [key]: value } } : metric,
    ),
  };
}

function noLocks() {
  return { fields: {}, metrics: {}, purpose: false, modelType: false };
}

function renderMetricsEditor(metrics, programs, softwareRequired) {
  return renderToStaticMarkup(
    React.createElement(MetricsEditor, {
      metrics,
      messages: [],
      setMetrics: () => {},
      programs,
      softwareRequired,
    }),
  );
}

/** The body createModel sends for the input. */
async function sentModelBody(input) {
  let body = null;
  await withFetch(
    async (_url, init) => {
      body = JSON.parse(init.body);
      return jsonResponse({
        data: {
          type: "model-revisions",
          id: "r",
          attributes: { entry_id: "entry-1", model_id: "m", revision_id: "r", state: "in_review" },
        },
      });
    },
    () => entries.createModel("token", "entry-1", input),
  );
  return body;
}

function modelPageFetch(runs) {
  return async (url) => {
    const path = new URL(String(url)).pathname;
    if (path === "/v1/entries/entry-1") {
      return jsonResponse({
        data: {
          type: "entries",
          id: "entry-1",
          attributes: {
            title: "Entry",
            polymer_entities: [],
            depositor_polymer_entities: [],
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
          relationships: {
            created_by: { data: { type: "users", id: "user-1" } },
            protein_sequences: { data: [] },
          },
        },
      });
    }
    if (path === "/v1/entries/entry-1/models/model-1") {
      return jsonResponse({ data: { type: "models", id: "model-1", attributes: storedModel() } });
    }
    if (path === "/v1/entries/entry-1/models/model-1/artifacts") {
      return jsonResponse({ data: [], meta: { runs, relations: [] } });
    }
    throw new Error(`unexpected fetch ${url}`);
  };
}

async function withFetch(fetch, action) {
  const previousFetch = global.fetch;
  const previousBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  global.fetch = fetch;
  process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
  try {
    return await action();
  } finally {
    global.fetch = previousFetch;
    if (previousBaseURL === undefined) {
      delete process.env.NEXT_PUBLIC_API_BASE_URL;
    } else {
      process.env.NEXT_PUBLIC_API_BASE_URL = previousBaseURL;
    }
  }
}

function jsonResponse(document) {
  return new Response(JSON.stringify(document), {
    status: 200,
    headers: { "Content-Type": "application/vnd.api+json" },
  });
}
