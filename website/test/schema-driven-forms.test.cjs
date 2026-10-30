require("./register.cjs");

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

const {
  ENTRY_COMPOUND_FIELDS,
  FIELDS,
  SECTIONS,
} = require("../src/app/components/entry-form/fields.ts");
const {
  apiFailure,
  fieldForPointer,
  placeApiErrors,
} = require("../src/app/components/entry-form/apiErrors.ts");
const {
  addCompound,
  addTypedCompounds,
  compoundListRequest,
  compoundReviewRequest,
  compoundsFromParsed,
  moveCompound,
  setCompoundListMode,
} = require("../src/app/components/entry-form/compounds.ts");
const {
  celsiusEcho,
  crystallographyRequest,
} = require("../src/app/components/entry-form/crystals.ts");
const {
  MUTATION_PATTERN,
  polymerEntitiesRequest,
  polymerEntityDraftsFromArchive,
} = require("../src/app/components/entry-form/polymerEntities.ts");
const {
  entryFormMessages,
  modelFormMessages,
} = require("../src/app/components/entry-form/validation.ts");
const {
  blockerLines,
  inlineMessages,
} = require("../src/app/components/entry-form/messages.ts");
const FilesEditor = require("../src/app/components/entry-form/FilesEditor.tsx").default;
const CompoundListsEditor = require("../src/app/components/entry-form/CompoundListsEditor.tsx").default;
const CrystalsEditor = require("../src/app/components/entry-form/CrystalsEditor.tsx").default;
const {
  buildCreateEntryInput,
  buildCreateModelInput,
  emptyProgramDraft,
  toProgramEntity,
} = require("../src/app/components/entry-form/helpers.ts");
const {
  emptyCompounds,
  emptyEntryMetadataDraft,
} = require("../src/app/components/entry-form/types.ts");
const drafts = require("../src/app/components/entry-form/drafts.ts");
const { detectFromFile } = require("../src/app/components/entry-form/autoDetect.ts");
const entries = require("../src/lib/api/entries.ts");

const OPENAPI_PATH = path.join(__dirname, "..", "..", "backend", "api", "openapi.yaml");

// ------------------------------------------------------------- registry --

test("should_place_every_field_in_a_section_the_pages_use", () => {
  // given
  const sections = new Set(Object.keys(SECTIONS));

  // when
  const misplaced = Object.entries(FIELDS).filter(
    ([, spec]) => !sections.has(spec.section),
  );

  // then
  assert.deepEqual(misplaced, []);
  assert.deepEqual(
    Object.values(SECTIONS).map((section) => section.title),
    ["Overview", "Macromolecules", "Experiment", "Data", "Evaluations"],
  );
});

test("should_mark_required_exactly_the_values_the_api_or_the_model_rule_requires", () => {
  // given: CreateEntryData requires title and method, CreateModelData title,
  // CreateRunRequest name; the form adds the one-model-file rule.
  const expected = ["entry.title", "entry.method", "model.title", "model.files", "run.name"];

  // when
  const required = Object.entries(FIELDS)
    .filter(([, spec]) => spec.required)
    .map(([id]) => id);

  // then
  assert.deepEqual(required.sort(), expected.sort());
});

test("should_check_mutations_with_the_pattern_the_api_declares", () => {
  // given
  const openapi = fs.readFileSync(OPENAPI_PATH, "utf8");

  // when / then
  assert.ok(
    openapi.includes(`pattern: "${MUTATION_PATTERN.source}"`),
    "DepositorPolymerEntity.mutations pattern changed in openapi.yaml",
  );
});

// ----------------------------------------------------------- API errors --

test("should_find_the_field_of_an_api_error_pointer", () => {
  // given
  const cases = [
    ["/entry/title", "entry.title"],
    ["/entry/external_refs/pdb", "entry.pdb"],
    ["/entry/polymer_entities/0/mutations/1", "entity.mutations"],
    ["/entry/crystallography/crystals/1/diffractions/0/temperature_kelvin", "diffraction.temperature"],
    ["/model/metadata/purpose", "model.purpose"],
    ["/model/metrics/2/value", "model.metrics"],
    ["/model/runs/0/software_version", "run.version"],
    ["/model/runs/0/name", "run.name"],
    ["/somewhere/else", null],
  ];

  // when / then
  for (const [pointer, field] of cases) {
    assert.equal(fieldForPointer(pointer), field, pointer);
  }
});

test("should_name_the_item_of_an_api_error_by_its_id_in_the_request", () => {
  // given
  const request = {
    entry: {
      polymer_entities: [{ label_entity_id: "2", mutations: ["C54X", "Q"] }],
      crystallography: {
        crystals: [
          { id: "1", diffractions: [{ id: "1" }] },
          { id: "2", diffractions: [{ id: "2" }, { id: "3" }] },
        ],
      },
    },
  };
  const errors = [
    { status: "422", detail: "must be one substitution", source: { pointer: "/entry/polymer_entities/0/mutations/1" } },
    { status: "422", detail: "must be above 0", source: { pointer: "/entry/crystallography/crystals/1/diffractions/1/temperature_kelvin" } },
    { status: "422", detail: "entry is locked" },
  ];

  // when
  const { placed, unplaced } = placeApiErrors(errors, request);

  // then
  assert.deepEqual(
    placed.map(({ field, item, message }) => ({ field, item, message })),
    [
      { field: "entity.mutations", item: "2", message: "must be one substitution" },
      { field: "diffraction.temperature", item: "2/3", message: "must be above 0" },
    ],
  );
  assert.deepEqual(unplaced, ["entry is locked"]);
});

test("should_key_model_errors_by_metric_key_and_run_id", () => {
  // given
  const request = {
    model: {
      metrics: [{ id: "m-1", key: "r_work", value: 0.2 }, { id: "m-2", key: "r_free", value: -1 }],
      runs: [{ id: "run-7", name: "phenix.refine", parameters: {} }],
    },
  };
  const errors = [
    { status: "422", detail: "must not be negative", source: { pointer: "/model/metrics/1/value" } },
    { status: "422", detail: "is required", source: { pointer: "/model/runs/0/software_version" } },
  ];

  // when
  const { placed } = placeApiErrors(errors, request);

  // then
  assert.deepEqual(
    placed.map(({ field, item }) => [field, item]),
    [["model.metrics", "r_free"], ["run.version", "run-7"]],
  );
});

test("should_keep_the_message_and_skip_the_request_when_the_api_sent_no_errors", () => {
  // given
  let built = false;
  const request = () => {
    built = true;
    return {};
  };

  // when
  const withoutErrors = apiFailure("Backend responded with 502", [], request);
  const withErrors = apiFailure(
    "Backend responded with 422",
    [{ status: "422", detail: "is required", source: { pointer: "/entry/method" } }],
    () => ({ entry: {} }),
  );

  // then
  assert.deepEqual(withoutErrors, { error: "Backend responded with 502", fieldErrors: [] });
  assert.equal(built, false);
  assert.equal(withErrors.fieldErrors[0].field, "entry.method");
  assert.match(withErrors.error, /marked below/);
});

test("should_show_an_api_error_under_its_file_when_the_pointer_runs_through_an_artifact", () => {
  // given
  const files = [
    uploadedFile({ id: "file-1", name: "data.mtz" }),
    uploadedFile({ id: "file-2", name: "seq.fasta" }),
  ];
  const request = {
    entry: {
      artifacts: [
        { id: "file-1", name: "data.mtz" },
        { id: "file-2", name: "seq.fasta" },
      ],
    },
  };
  const errors = [
    { status: "422", detail: "format is not allowed", source: { pointer: "/entry/artifacts/1/format" } },
  ];

  // when
  const { fieldErrors } = apiFailure("Backend responded with 422", errors, () => request);
  const html = renderToStaticMarkup(
    React.createElement(FilesEditor, {
      files,
      onAdd: () => {},
      onAddUrl: () => {},
      onRemove: () => {},
      onLevel: () => {},
      onPatch: () => {},
      fileMessages: (fileId) => inlineMessages(fieldErrors, "entry.files", fileId),
    }),
  );

  // then
  const [, firstFile, secondFile] = html.split("<li");
  assert.ok(!firstFile.includes("Format is not allowed."));
  assert.ok(secondFile.includes("Format is not allowed."));
});

// ----------------------------------------------------- ligands, cofactors --

test("should_sort_typed_compounds_by_the_reference_lists", () => {
  // given
  let compounds = emptyCompounds();

  // when
  for (const id of ["hem", "ZN", "ATP", "GOL"]) {
    const result = addCompound(compounds, id);
    assert.equal(result.error, null, id);
    compounds = result.compounds;
  }

  // then
  assert.deepEqual(compoundListRequest(compounds.cofactors), ["HEM", "ZN"]);
  assert.deepEqual(compoundListRequest(compounds.ligands), ["ATP", "GOL"]);
  assert.deepEqual(
    compoundReviewRequest(compounds).map((item) => [item.list, item.id]),
    [["ligands", "GOL"]],
  );
});

test("should_give_the_matching_hint_when_an_ambiguous_compound_or_a_buffer_is_typed", () => {
  // given
  let compounds = emptyCompounds();

  // when
  for (const id of ["SO4", "cl", "GOL"]) {
    compounds = addCompound(compounds, id).compounds;
  }

  // then
  assert.deepEqual(compoundReviewRequest(compounds), [
    { list: "ligands", id: "SO4", reason: "often from the buffer: confirm it is part of the sample" },
    { list: "ligands", id: "CL", reason: "often from the buffer: confirm it is part of the sample" },
    { list: "ligands", id: "GOL", reason: "usually a solvent or buffer, which the sample composition leaves out" },
  ]);
});

test("should_refuse_water_duplicates_and_text_that_is_not_a_ccd_id", () => {
  // given
  const compounds = addCompound(emptyCompounds(), "ATP").compounds;

  // when
  const results = ["HOH", "atp", "NOT-A-CCD"].map((raw) => addCompound(compounds, raw));

  // then
  for (const result of results) {
    assert.equal(result.compounds, compounds);
    assert.ok(result.error);
  }
});

test("should_tell_not_known_from_none_from_a_list", () => {
  // given
  const listed = addCompound(emptyCompounds(), "ATP").compounds;

  // when
  const none = setCompoundListMode(listed, "ligands", "none");
  const unknown = setCompoundListMode(listed, "ligands", "unknown");

  // then
  assert.deepEqual(compoundListRequest(listed.ligands), ["ATP"]);
  assert.deepEqual(compoundListRequest(none.ligands), []);
  assert.equal(compoundListRequest(unknown.ligands), undefined);
});

test("should_explain_not_known_and_none_and_that_none_is_not_apo", () => {
  // given
  const compounds = setCompoundListMode(emptyCompounds(), "cofactors", "none");
  const apoNote = "None does not by itself mean the sample is apo.";

  // when
  const html = renderToStaticMarkup(
    React.createElement(CompoundListsEditor, {
      compounds,
      onChange: () => {},
      fields: ENTRY_COMPOUND_FIELDS,
      messages: [],
    }),
  );

  // then
  const [ligands, cofactors] = html.split("Not known").slice(1);
  assert.ok(ligands.includes("Not assessed."));
  assert.ok(cofactors.includes("Assessed; none present."));
  assert.ok(!html.includes("We have not checked."));
  assert.ok(!html.includes("Checked, and none are present."));
  assert.ok(cofactors.includes(apoNote));
  assert.ok(!ligands.includes(apoNote));
  assert.ok(FIELDS["model.cofactors"].help.endsWith(apoNote));
});

test("should_say_assessed_present_as_listed_above_the_hint_or_the_chips", () => {
  // given: ligands Listed, once still empty and once with ATP
  const empty = setCompoundListMode(emptyCompounds(), "ligands", "list");
  const withAtp = addCompound(emptyCompounds(), "ATP").compounds;
  const listedNote = "Assessed; present as listed.";
  const emptyHint = "Add CCD IDs above. An empty list is saved as none.";

  // when
  const [emptyLigands, withAtpLigands] = [empty, withAtp].map((compounds) => {
    const html = renderToStaticMarkup(
      React.createElement(CompoundListsEditor, {
        compounds,
        onChange: () => {},
        fields: ENTRY_COMPOUND_FIELDS,
        messages: [],
      }),
    );
    return html.split("Not known")[1];
  });

  // then
  assert.ok(emptyLigands.indexOf(listedNote) >= 0);
  assert.ok(emptyLigands.indexOf(listedNote) < emptyLigands.indexOf(emptyHint));
  assert.ok(withAtpLigands.indexOf(listedNote) >= 0);
  assert.ok(withAtpLigands.indexOf(listedNote) < withAtpLigands.indexOf("<code>ATP</code>"));
  assert.ok(!withAtpLigands.includes(emptyHint));
});

test("should_clear_the_review_flag_when_a_compound_is_moved", () => {
  // given: a metal from a file without metalc rows
  const compounds = compoundsFromParsed(
    { ligands: ["NAG"], cofactors: ["ZN"] },
    [{ list: "cofactors", id: "ZN", reason: "metal ion kept for review" }],
  );

  // when
  const moved = moveCompound(compounds, "cofactors", "ZN");

  // then
  assert.deepEqual(compounds.cofactors.items, [{ id: "ZN", review: "metal ion kept for review" }]);
  assert.deepEqual(moved.ligands.items.map((item) => [item.id, item.review]), [["NAG", null], ["ZN", null]]);
  assert.equal(moved.cofactors.items.length, 0);
});

test("should_add_the_valid_ids_and_keep_the_refused_ones_when_typed_text_is_committed", () => {
  // given
  const compounds = addCompound(emptyCompounds(), "ATP").compounds;

  // when
  const result = addTypedCompounds(compounds, "hem, atp; NOT-A-CCD zn");
  const blank = addTypedCompounds(compounds, "  ");

  // then
  assert.deepEqual(compoundListRequest(result.compounds.cofactors), ["HEM", "ZN"]);
  assert.deepEqual(compoundListRequest(result.compounds.ligands), ["ATP"]);
  assert.equal(result.rejected, "atp NOT-A-CCD");
  assert.equal(result.error, "ATP is already listed");
  assert.deepEqual(blank, { compounds, rejected: "", error: null });
});

test("should_report_a_bad_id_on_its_own_list_when_either_form_is_checked", () => {
  // given: IDs restored from a draft, which skips the typed-ID checks
  const compounds = {
    ligands: { mode: "list", items: [{ id: "NOT-A-CCD", review: null }] },
    cofactors: { mode: "list", items: [{ id: "HOH", review: null }] },
  };
  const compoundFields = new Set([
    "entry.ligands",
    "entry.cofactors",
    "model.ligands",
    "model.cofactors",
  ]);
  const compoundMessages = (messages) =>
    messages
      .filter((message) => compoundFields.has(message.field))
      .map((message) => message.field);

  // when
  const entryMessages = entryFormMessages("Entry", { ...emptyEntryMetadataDraft(), compounds });
  const modelMessages = modelFormMessages(modelDraft({ compounds }));

  // then
  assert.deepEqual(compoundMessages(entryMessages), ["entry.ligands", "entry.cofactors"]);
  assert.deepEqual(compoundMessages(modelMessages), ["model.ligands", "model.cofactors"]);
});

// ------------------------------------------------- crystals and entities --

test("should_number_crystals_and_datasets_like_the_archive_and_leave_blanks_out", () => {
  // given
  const crystals = [
    { key: "a", ph: "7.5", growthTemperature: "", diffractions: [{ key: "d1", temperature: "100" }] },
    { key: "b", ph: "", growthTemperature: "293", diffractions: [{ key: "d2", temperature: "100" }, { key: "d3", temperature: "293" }] },
  ];

  // when
  const request = crystallographyRequest(crystals);

  // then
  assert.deepEqual(request, {
    crystals: [
      { id: "1", growth: { ph: 7.5 }, diffractions: [{ id: "1", temperature_kelvin: 100 }] },
      {
        id: "2",
        growth: { temperature_kelvin: 293 },
        diffractions: [
          { id: "2", temperature_kelvin: 100 },
          { id: "3", temperature_kelvin: 293 },
        ],
      },
    ],
  });
  assert.equal(crystallographyRequest([]), undefined);
});

test("should_warn_but_never_block_on_temperatures_outside_the_wwpdb_ranges", () => {
  // given
  const crystal = (growthTemperature, collectionTemperatures) => ({
    key: `c-${growthTemperature}`,
    ph: "",
    growthTemperature,
    diffractions: collectionTemperatures.map((temperature, index) => ({
      key: `d-${growthTemperature}-${index}`,
      temperature,
    })),
  });
  const metadata = {
    ...emptyEntryMetadataDraft(),
    method: "X-ray crystallography",
    crystals: [
      crystal("293", ["100"]),
      crystal("277", ["80"]),
      crystal("316", ["300"]),
      crystal("20", ["50"]),
      crystal("250", ["310"]),
      crystal("4", []),
      crystal("43", []),
    ],
  };
  const growthLine = "outside the usual 277–316 K for crystal growth; keep it only if it is right";
  const collectionLine = "outside the usual 80–300 K for data collection; keep it only if it is right";

  // when
  const messages = entryFormMessages("Lysozyme", metadata);

  // then
  assert.deepEqual(messages.filter((message) => message.blocking), []);
  assert.deepEqual(
    messages.map((message) => [message.field, message.item, message.message]),
    [
      ["crystal.growthTemperature", "4", "this looks like °C; 20 °C is 293.15 K"],
      ["diffraction.temperature", "4/4", collectionLine],
      ["crystal.growthTemperature", "5", growthLine],
      ["diffraction.temperature", "5/5", collectionLine],
      ["crystal.growthTemperature", "6", "this looks like °C; 4 °C is 277.15 K"],
      ["crystal.growthTemperature", "7", growthLine],
    ],
  );
});

test("should_only_block_a_temperature_at_or_below_0_k", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    method: "X-ray crystallography",
    crystals: [{ key: "a", ph: "", growthTemperature: "0", diffractions: [{ key: "d", temperature: "-5" }] }],
  };

  // when
  const messages = entryFormMessages("Lysozyme", metadata);

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.item, message.blocking]),
    [
      ["crystal.growthTemperature", "1", true],
      ["diffraction.temperature", "1/1", true],
    ],
  );
});

test("should_echo_a_kelvin_value_in_celsius_as_it_is_typed", () => {
  // when / then
  assert.equal(celsiusEcho("293.15"), "20 °C");
  assert.equal(celsiusEcho("293"), "19.85 °C");
  assert.equal(celsiusEcho(" 100 "), "-173.15 °C");
  for (const unfinished of ["", "-", "."]) {
    assert.equal(celsiusEcho(unfinished), null, JSON.stringify(unfinished));
  }
});

test("should_show_k_beside_each_temperature_box_and_the_celsius_value_under_it", () => {
  // given
  const crystals = [
    {
      key: "a",
      ph: "",
      growthTemperature: "293.15",
      diffractions: [
        { key: "d1", temperature: "100" },
        { key: "d2", temperature: "" },
      ],
    },
  ];

  // when
  const html = renderToStaticMarkup(
    React.createElement(CrystalsEditor, { crystals, onChange: () => {}, messages: [] }),
  );

  // then
  assert.equal(html.match(/>K<\/span>/g).length, 3);
  assert.deepEqual(
    [...html.matchAll(/>(= [^<]*)</g)].map((match) => match[1]),
    ["= 20 °C", "= -173.15 °C"],
  );
});

test("should_send_only_typed_entity_values_and_the_organism_only_when_the_pdb_has_none", () => {
  // given
  const proposals = [
    { label_entity_id: "1", description: "Lysozyme", source_organisms: [{ scientific_name: "Gallus gallus", ncbi_taxonomy_id: 9031 }], uniprot_mappings: [{ accession: "P00698", source: "sifts" }], construct: "full length", mutations: ["C54T"] },
    { label_entity_id: "2", description: "Nanobody", source_organisms: [], uniprot_mappings: [] },
    { label_entity_id: "3", description: "Untouched", source_organisms: [], uniprot_mappings: [] },
  ];
  const typed = [{ labelEntityId: "1", construct: "1-129", mutations: ["C54A"], organismName: "Homo sapiens", organismTaxonomyId: "9606" }];

  // when
  const entities = polymerEntityDraftsFromArchive(proposals, typed);
  entities[1] = { ...entities[1], organismName: "Lama glama", organismTaxonomyId: "9844" };
  const request = polymerEntitiesRequest(entities);

  // then
  assert.equal(entities[0].archiveConstruct, "full length");
  assert.deepEqual(entities[0].uniprotAccessions, ["P00698"]);
  assert.deepEqual(request, [
    { label_entity_id: "1", construct: "1-129", mutations: ["C54A"] },
    { label_entity_id: "2", source_organisms: [{ scientific_name: "Lama glama", ncbi_taxonomy_id: 9844 }] },
  ]);
  assert.equal(polymerEntitiesRequest([]), undefined);
});

test("should_build_the_entry_input_with_every_depositor_field", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    details: " Room temperature ",
    pdb: "5gy3",
    resolution: "1.77",
    method: " X-ray crystallography ",
    spaceGroup: "P 21 21 21",
    compounds: setCompoundListMode(addCompound(emptyCompounds(), "HEM").compounds, "ligands", "none"),
    crystals: [{ key: "a", ph: "7", growthTemperature: "", diffractions: [] }],
    polymerEntities: [{ labelEntityId: "1", description: "", uniprotAccessions: [], archiveOrganisms: ["Gallus gallus"], archiveConstruct: "", archiveMutations: [], construct: "", mutations: ["C54T"], organismName: "", organismTaxonomyId: "" }],
  };

  // when
  const input = buildCreateEntryInput(" Lysozyme ", metadata, null, []);
  const withoutPdb = buildCreateEntryInput("Lysozyme", { ...metadata, pdb: "" }, null, []);

  // then
  assert.deepEqual(JSON.parse(JSON.stringify(input)), {
    title: "Lysozyme",
    thumbnail_image_url: null,
    details: "Room temperature",
    external_refs: { pdb: "5GY3" },
    method: "X-ray crystallography",
    resolution: 1.77,
    space_group: "P 21 21 21",
    crystallography: { crystals: [{ id: "1", growth: { ph: 7 } }] },
    ligands: [],
    cofactors: ["HEM"],
    polymer_entities: [{ label_entity_id: "1", mutations: ["C54T"] }],
    entities: [],
  });
  assert.equal(withoutPdb.polymer_entities, undefined);
});

test("should_forward_the_new_entry_fields_to_the_create_request", () => {
  // given
  const input = {
    title: "Lysozyme",
    method: "X-ray crystallography",
    ligands: [],
    cofactors: ["HEM"],
    polymer_entities: [{ label_entity_id: "1", construct: "1-129" }],
  };

  // when
  const body = entries.createEntryRequestBody(input);

  // then
  assert.deepEqual(body.entry.ligands, []);
  assert.deepEqual(body.entry.cofactors, ["HEM"]);
  assert.deepEqual(body.entry.polymer_entities, [{ label_entity_id: "1", construct: "1-129" }]);
});

// -------------------------------------------------------------- model --

test("should_send_run_command_and_parameters_and_let_the_form_override_file_composition", () => {
  // given
  const program = {
    ...emptyProgramDraft({ id: "00000000-0000-4000-8000-000000000010", name: "qFit", version: "3.2" }),
    command: " qfit_protein model.cif map.ccp4 ",
    parameters: '{"cycles": 5}',
  };
  const modelEntity = {
    id: "00000000-0000-4000-8000-000000000011",
    type: "model",
    artifact_type: "model",
    level: "L2",
    name: "model.cif",
    payload: {
      file_url: "https://files.example/model.cif",
      metadata: {
        ligands: ["GOL"],
        extensions: { compound_review: [{ list: "ligands", id: "GOL", reason: "solvent" }] },
      },
    },
  };

  // when
  const body = entries.createModelRequestBody({
    title: "qFit model",
    entities: [modelEntity, toProgramEntity(program)],
    metadata: { ligands: undefined, cofactors: [], extensions: { compound_review: undefined } },
  });
  const wire = JSON.parse(JSON.stringify(body));

  // then
  assert.equal(wire.model.runs[0].command, "qfit_protein model.cif map.ccp4");
  assert.deepEqual(wire.model.runs[0].parameters, { cycles: 5 });
  assert.equal(wire.model.metadata.ligands, undefined);
  assert.deepEqual(wire.model.metadata.cofactors, []);
  assert.equal(wire.model.metadata.extensions, undefined);
});

test("should_put_model_attribution_and_composition_in_the_model_metadata", () => {
  // given
  const draft = modelDraft({
    details: " Built with qFit ",
    authors: ["Keedy, D.", " "],
    affiliation: " CUNY ",
    compounds: compoundsFromParsed({ ligands: ["GOL"], cofactors: [] }, [
      { list: "ligands", id: "GOL", reason: "solvent" },
    ]),
  });

  // when
  const metadata = JSON.parse(JSON.stringify(buildCreateModelInput(draft).metadata));

  // then
  assert.deepEqual(metadata, {
    details: "Built with qFit",
    authors: ["Keedy, D."],
    affiliation: "CUNY",
    ligands: ["GOL"],
    cofactors: [],
    extensions: { compound_review: [{ list: "ligands", id: "GOL", reason: "solvent" }] },
  });
});

// ---------------------------------------------------------- validation --

test("should_list_missing_values_beside_the_button_but_not_under_the_field", () => {
  // given
  const metadata = emptyEntryMetadataDraft();

  // when
  const messages = entryFormMessages(" ", metadata);

  // then
  assert.deepEqual(blockerLines(messages), ["Title: required", "Method: required"]);
  assert.ok(messages.every((message) => message.missing));
});

test("should_block_bad_formats_and_only_advise_on_an_unusual_space_group", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    method: "X-ray crystallography",
    pdb: "ABCD",
    resolution: "-1",
    spaceGroup: "orthorhombic",
    crystals: [
      { key: "a", ph: "15", growthTemperature: "", diffractions: [{ key: "d", temperature: "" }] },
      { key: "b", ph: "", growthTemperature: "", diffractions: [] },
    ],
    polymerEntities: [{ labelEntityId: "1", description: "", uniprotAccessions: [], archiveOrganisms: [], archiveConstruct: "", archiveMutations: [], construct: "", mutations: ["C54"], organismName: "", organismTaxonomyId: "9606" }],
  };

  // when
  const messages = entryFormMessages("Lysozyme", metadata);

  // then
  const blocking = messages.filter((message) => message.blocking).map((message) => [message.field, message.item]);
  assert.deepEqual(blocking, [
    ["entry.pdb", null],
    ["entity.mutations", "1"],
    ["entity.organism", "1"],
    ["entry.resolution", null],
    ["crystal.ph", "1"],
    ["diffraction.temperature", "1/1"],
    ["crystal.block", "2"],
  ]);
  const advice = messages.filter((message) => !message.blocking);
  assert.deepEqual(advice.map((message) => message.field), ["entry.spaceGroup"]);
});

test("should_accept_hermann_mauguin_space_groups", () => {
  // given
  const groups = ["P 21 21 21", "P212121", "C 1 2 1", "P 63/m m c", "I 41/a m d", "R 3 :H", "P -1", "F d -3 m"];

  // when / then
  for (const spaceGroup of groups) {
    const messages = entryFormMessages("T", { ...emptyEntryMetadataDraft(), method: "X", spaceGroup });
    assert.deepEqual(messages, [], spaceGroup);
  }
});

test("should_check_model_files_steps_parameters_metrics_and_hidden_choices", () => {
  // given
  const draft = modelDraft({
    files: [],
    purpose: "Reference",
    metrics: [{ id: "m", values: { r_work: "." } }],
    programs: [{ ...emptyProgramDraft({ id: "p-1" }), parameters: "[1, 2]" }],
  });

  // when
  const messages = modelFormMessages(draft);

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.item, Boolean(message.missing)]),
    [
      ["model.purpose", null, false],
      ["model.files", null, true],
      ["model.metrics", "r_work", false],
      ["run.name", "p-1", false],
      ["run.parameters", "p-1", false],
    ],
  );
});

// --------------------------------------------------------------- drafts --

test("should_restore_a_version_3_entry_draft_with_an_empty_metadata_block", () => {
  withLocalStorage(() => {
    // given
    window.localStorage.setItem(
      "dpdb:new-entry-draft",
      JSON.stringify({ version: 3, entryId: "e-1", title: "Old", thumbUrl: null, thumbUploadStatus: "idle", files: [] }),
    );

    // when
    const draft = drafts.readStoredDraft();

    // then
    assert.equal(draft.version, 4);
    assert.equal(draft.title, "Old");
    assert.deepEqual(draft.metadata, emptyEntryMetadataDraft());
  });
});

test("should_save_and_restore_the_entry_metadata_block", () => {
  withLocalStorage(() => {
    // given
    const metadata = { ...emptyEntryMetadataDraft(), pdb: "5GY3", method: "CryoEM" };

    // when
    drafts.writeStoredDraft({ version: 4, entryId: "e-1", metadata, title: "", thumbUrl: null, thumbUploadStatus: "idle", files: [] });
    const restored = drafts.readStoredDraft();

    // then
    assert.deepEqual(restored.metadata, metadata);
    assert.equal(drafts.draftHasContent(restored), true);
  });
});

test("should_keep_model_drafts_per_entry_without_unfinished_uploads", () => {
  withLocalStorage(() => {
    // given
    const draft = modelDraft({
      title: "qFit model",
      files: [
        uploadedFile({ id: "f-1", url: "https://files.example/model.cif" }),
        uploadedFile({ id: "f-2", url: "", uploadStatus: "uploading" }),
      ],
    });

    // when
    drafts.writeStoredModelDraft("dpdb_aaaaaaaa", drafts.modelDraftToStored(draft));
    const restored = drafts.readStoredModelDraft("dpdb_aaaaaaaa");
    const other = drafts.readStoredModelDraft("dpdb_bbbbbbbb");

    // then
    assert.equal(other, null);
    const model = drafts.modelDraftFromStored(restored);
    assert.equal(model.title, "qFit model");
    assert.deepEqual(model.files.map((file) => file.id), ["f-1"]);
    assert.equal(drafts.storedModelDraftHasContent(restored), true);
  });
});

// ----------------------------------------------------------- autoDetect --

test("should_fill_model_authors_and_composition_from_the_coordinate_header", async () => {
  // given
  const pdb = [
    "HEADER    HYDROLASE                               01-JUN-16   5GY3",
    "AUTHOR    A.ATTIGANI,S.P.LI",
    hetatm(1, "FE", "HEM", 401, "FE"),
    hetatm(2, "ZN", "ZN", 402, "ZN"),
    "END",
  ].join("\n");
  const file = new File([pdb], "model.pdb");
  const parsed = uploadedFile({ id: "f-1", name: "model.pdb", type: "pdb" });

  // when
  const patch = await detectFromFile(file, parsed, modelDraft({ files: [parsed] }));

  // then
  assert.deepEqual(patch.authors, ["Attigani, A.", "Li, S.P."]);
  assert.equal(patch.compoundsSource, "model.pdb");
  assert.deepEqual(patch.compounds.cofactors.items.map((item) => item.id).sort(), ["HEM", "ZN"]);
  assert.ok(
    patch.compounds.cofactors.items.find((item) => item.id === "ZN").review,
    "a metal in a file without metalc rows is kept for review",
  );
});

// -------------------------------------------------------------- helpers --

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
    files: [uploadedFile({ id: "model-file-1" })],
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

function uploadedFile(overrides = {}) {
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

function hetatm(serial, name, resName, resSeq, element) {
  return (
    "HETATM" +
    String(serial).padStart(5) +
    " " +
    name.padEnd(4) +
    " " +
    resName.padStart(3) +
    " A" +
    String(resSeq).padStart(4) +
    "    " +
    "  20.154  29.699   5.276  1.00 49.05" +
    " ".repeat(10) +
    element.padStart(2)
  );
}

function withLocalStorage(run) {
  const store = new Map();
  globalThis.window = {
    localStorage: {
      getItem: (key) => (store.has(key) ? store.get(key) : null),
      setItem: (key, value) => store.set(key, String(value)),
      removeItem: (key) => store.delete(key),
    },
  };
  try {
    run();
  } finally {
    delete globalThis.window;
  }
}
