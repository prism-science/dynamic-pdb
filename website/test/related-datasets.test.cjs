require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

const { fieldForPointer } = require("../src/app/components/entry-form/apiErrors.ts");
const { buildCreateEntryInput } = require("../src/app/components/entry-form/helpers.ts");
const {
  normalizeDoi,
  relatedDatasetDraftsFromStored,
  relatedDatasetsRequest,
} = require("../src/app/components/entry-form/relatedDatasets.ts");
const {
  buildEntryRevisionChange,
  entryDraftFromEntry,
} = require("../src/app/components/entry-form/revisionChanges.ts");
const { emptyEntryMetadataDraft } = require("../src/app/components/entry-form/types.ts");
const { entryFormMessages } = require("../src/app/components/entry-form/validation.ts");
const { inlineMessages } = require("../src/app/components/entry-form/messages.ts");
const { diffEntry } = require("../src/lib/reviewDiff.ts");
const RelatedDatasetsEditor =
  require("../src/app/components/entry-form/RelatedDatasetsEditor.tsx").default;
const RelatedDatasets = require("../src/app/components/RelatedDatasets.tsx").default;

function draft(overrides = {}) {
  return {
    key: "row-1",
    doi: "10.15785/SBGRID/1303",
    dataType: "diffraction image data",
    repository: "SBGrid Data Bank",
    details: "",
    ...overrides,
  };
}

function storedEntry(overrides = {}) {
  return {
    id: "dpdb_entry1",
    created_by: "user-1",
    title: "Lysozyme",
    thumbnail_image_url: null,
    method: "X-ray crystallography",
    related_datasets: [
      { doi: "10.15785/SBGRID/1303", data_type: "diffraction image data", repository: "SBGrid Data Bank" },
    ],
    created_at: "2026-10-30T00:00:00Z",
    updated_at: "2026-10-30T00:00:00Z",
    ...overrides,
  };
}

test("should_strip_resolver_url_or_doi_prefix_like_the_api", () => {
  // given
  const inputs = {
    "  10.15785/SBGRID/1303 ": "10.15785/SBGRID/1303",
    "https://doi.org/10.15785/SBGRID/1303": "10.15785/SBGRID/1303",
    "HTTP://DX.DOI.ORG/10.15785/SBGRID/1303": "10.15785/SBGRID/1303",
    "doi: 10.15785/SBGRID/1303": "10.15785/SBGRID/1303",
    "https://zenodo.org/records/1234": "https://zenodo.org/records/1234",
  };

  for (const [input, expected] of Object.entries(inputs)) {
    // when
    const normalized = normalizeDoi(input);

    // then
    assert.equal(normalized, expected, input);
  }
});

test("should_send_trimmed_datasets_and_leave_empty_optional_values_out", () => {
  // given
  const drafts = [
    draft({ doi: " https://doi.org/10.15785/SBGRID/1303 ", repository: "  ", details: " Similar crystals. " }),
  ];

  // when
  const request = relatedDatasetsRequest(drafts);

  // then
  assert.deepEqual(request, [
    {
      doi: "10.15785/SBGRID/1303",
      data_type: "diffraction image data",
      repository: undefined,
      details: "Similar crystals.",
    },
  ]);
  assert.equal(relatedDatasetsRequest([]), undefined);
});

test("should_send_related_datasets_with_a_new_entry", () => {
  // given
  const metadata = { ...emptyEntryMetadataDraft(), method: "X-ray crystallography", relatedDatasets: [draft()] };

  // when
  const input = buildCreateEntryInput("Lysozyme", metadata, null, []);

  // then
  assert.deepEqual(input.related_datasets, [
    { doi: "10.15785/SBGRID/1303", data_type: "diffraction image data", repository: "SBGrid Data Bank", details: undefined },
  ]);
});

test("should_name_the_row_of_each_broken_rule_when_the_form_is_checked", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    method: "X-ray crystallography",
    relatedDatasets: [
      draft({ key: "a", doi: "", dataType: "" }),
      draft({ key: "b", doi: "https://zenodo.org/records/1234" }),
      draft({ key: "c" }),
      draft({ key: "d", doi: "https://doi.org/10.15785/sbgrid/1303", details: "x".repeat(2001) }),
    ],
  };

  // when
  const messages = entryFormMessages("Lysozyme", metadata).filter((message) =>
    message.field.startsWith("relatedDataset."),
  );

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.item, message.message]),
    [
      ["relatedDataset.doi", "1", "required"],
      ["relatedDataset.dataType", "1", "required"],
      ["relatedDataset.doi", "2", "use a DOI such as 10.15785/SBGRID/1303"],
      ["relatedDataset.doi", "4", "this dataset is already listed"],
      ["relatedDataset.details", "4", "use at most 2000 characters"],
    ],
  );
});

test("should_place_api_errors_about_a_dataset_on_its_field", () => {
  // when / then
  assert.equal(fieldForPointer("/entry/related_datasets/0/doi"), "relatedDataset.doi");
  assert.equal(fieldForPointer("/entry/related_datasets/1/data_type"), "relatedDataset.dataType");
  assert.equal(fieldForPointer("/entry/related_datasets"), "entry.relatedDatasets");
});

test("should_send_no_related_datasets_key_when_the_edit_leaves_them_alone", () => {
  // given
  const entry = storedEntry();
  const initial = entryDraftFromEntry(entry);
  const current = { ...initial, title: "Lysozyme, retitled" };

  // when
  const change = buildEntryRevisionChange(entry, initial, current);

  // then
  assert.deepEqual(Object.keys(change), ["title"]);
});

test("should_send_the_whole_list_when_a_dataset_is_added_and_null_when_all_are_removed", () => {
  // given
  const entry = storedEntry();
  const initial = entryDraftFromEntry(entry);
  const added = {
    ...initial,
    metadata: {
      ...initial.metadata,
      relatedDatasets: [
        ...initial.metadata.relatedDatasets,
        draft({ key: "new", doi: "doi:10.5281/zenodo.1234", dataType: "EM raw micrographs", repository: "" }),
      ],
    },
  };
  const removed = { ...initial, metadata: { ...initial.metadata, relatedDatasets: [] } };

  // when
  const addChange = buildEntryRevisionChange(entry, initial, added);
  const removeChange = buildEntryRevisionChange(entry, initial, removed);

  // then
  assert.deepEqual(addChange.related_datasets.map((dataset) => dataset.doi), [
    "10.15785/SBGRID/1303",
    "10.5281/zenodo.1234",
  ]);
  assert.deepEqual(removeChange, { related_datasets: null });
});

test("should_show_one_review_row_per_doi_when_datasets_change", () => {
  // given
  const active = {
    ...storedEntry(),
    protein_sequences: [],
    artifacts: [],
  };
  const proposed = {
    ...active,
    related_datasets: [
      { doi: "10.15785/sbgrid/1303", data_type: "diffraction image data", repository: "SBGrid Data Bank" },
      { doi: "10.5281/zenodo.1234", data_type: "EM raw micrographs" },
    ],
  };

  // when
  const section = diffEntry(active, proposed).find((item) => item.title === "Related datasets");

  // then
  assert.deepEqual(
    section.rows.map((row) => [row.label, row.state]),
    [
      ["10.15785/sbgrid/1303", "changed"],
      ["10.5281/zenodo.1234", "added"],
    ],
  );
});

test("should_show_each_row_with_its_own_messages_and_api_errors_without_an_item_above_the_rows", () => {
  // given
  const datasets = relatedDatasetDraftsFromStored(storedEntry().related_datasets);
  const messages = [
    { field: "relatedDataset.doi", item: "1", message: "use a DOI such as 10.15785/SBGRID/1303", blocking: true },
    { field: "relatedDataset.dataType", item: null, message: "must not be blank", blocking: true },
  ];

  // when
  const html = renderToStaticMarkup(
    React.createElement(RelatedDatasetsEditor, { datasets, onChange: () => {}, messages }),
  );

  // then
  assert.ok(html.includes('value="10.15785/SBGRID/1303"'));
  assert.ok(html.includes("Dataset 1"));
  assert.ok(html.includes("Use a DOI such as 10.15785/SBGRID/1303"));
  assert.ok(html.includes("Must not be blank"));
  assert.equal(inlineMessages(messages, "relatedDataset.dataType", "1").length, 0);
  assert.ok(html.includes("Add related dataset"));
});

test("should_link_each_doi_to_its_resolver_on_the_entry_page", () => {
  // when
  const html = renderToStaticMarkup(
    React.createElement(RelatedDatasets, { datasets: storedEntry().related_datasets }),
  );

  // then
  assert.ok(html.includes('href="https://doi.org/10.15785/SBGRID/1303"'));
  assert.ok(html.includes("diffraction image data · SBGrid Data Bank"));
});
