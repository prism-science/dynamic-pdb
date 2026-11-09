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
const drafts = require("../src/app/components/entry-form/drafts.ts");
const { DRAFT_STORAGE_KEY } = require("../src/app/components/entry-form/types.ts");
const { isLandingUrl } = require("../src/lib/related-datasets.ts");
const { diffEntry } = require("../src/lib/reviewDiff.ts");
const RelatedDatasetsEditor =
  require("../src/app/components/entry-form/RelatedDatasetsEditor.tsx").default;
const RelatedDatasets = require("../src/app/components/RelatedDatasets.tsx").default;

function draft(overrides = {}) {
  return {
    key: "row-1",
    doi: "10.15785/SBGRID/1303",
    accession: "",
    landingUrl: "",
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
      accession: undefined,
      landing_url: undefined,
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
    {
      doi: "10.15785/SBGRID/1303",
      accession: undefined,
      landing_url: undefined,
      data_type: "diffraction image data",
      repository: "SBGrid Data Bank",
      details: undefined,
    },
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
  const messages = entryFormMessages("Lysozyme", metadata, "").filter((message) =>
    message.field.startsWith("relatedDataset."),
  );

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.item, message.message]),
    [
      ["relatedDataset.doi", "1", "add a DOI or an accession"],
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

// ------------------------------------------------- accession, landing page --

test("should_send_a_dataset_named_only_by_accession_without_a_doi", () => {
  // given
  const drafts = [
    draft({
      doi: "  ",
      accession: " EMPIAR-10028 ",
      landingUrl: " https://www.ebi.ac.uk/empiar/EMPIAR-10028/ ",
      dataType: "EM raw micrographs",
      repository: "EMPIAR",
    }),
  ];

  // when
  const request = relatedDatasetsRequest(drafts);

  // then
  assert.deepEqual(request, [
    {
      doi: undefined,
      accession: "EMPIAR-10028",
      landing_url: "https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
      data_type: "EM raw micrographs",
      repository: "EMPIAR",
      details: undefined,
    },
  ]);
});

test("should_accept_the_landing_pages_the_api_accepts", () => {
  // given
  const accepted = [
    "https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
    "http://example.org:8080/data?id=1#top",
    "HTTPS://Zenodo.org/records/1234",
  ];
  const refused = [
    "ftp://ftp.ebi.ac.uk/empiar/world_availability/10028/",
    "javascript:alert(1)",
    "www.ebi.ac.uk/empiar/EMPIAR-10028/",
    "https://",
    "https:example.org",
    "https:/example.org",
    "https://www.ebi.ac.uk/empiar/EMPIAR 10028/",
    "https://user:secret@example.org/data",
    "https://example.org:port/",
  ];

  // when / then
  for (const url of accepted) assert.equal(isLandingUrl(url), true, url);
  for (const url of refused) assert.equal(isLandingUrl(url), false, url);
});

test("should_name_the_row_of_each_broken_accession_or_landing_page_rule", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    method: "X-ray crystallography",
    relatedDatasets: [
      draft({ key: "a", doi: "", accession: "EMPIAR-10028", repository: "EMPIAR" }),
      draft({ key: "b", doi: "", accession: "empiar-10028", repository: " empiar " }),
      draft({ key: "c", doi: "", accession: "EMPIAR-10028", repository: "Zenodo" }),
      draft({ key: "d", doi: "", accession: "x".repeat(257) }),
      draft({ key: "e", landingUrl: "javascript:alert(1)" }),
      draft({ key: "f", doi: "10.5281/zenodo.1", landingUrl: `https://example.org/${"a".repeat(2048)}` }),
    ],
  };

  // when
  const messages = entryFormMessages("Lysozyme", metadata, "").filter((message) =>
    message.field.startsWith("relatedDataset."),
  );

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.item, message.message]),
    [
      ["relatedDataset.accession", "2", "this dataset is already listed"],
      ["relatedDataset.accession", "4", "use at most 256 characters"],
      [
        "relatedDataset.landingUrl",
        "5",
        "use an http or https link such as https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
      ],
      ["relatedDataset.landingUrl", "6", "use at most 2048 characters"],
    ],
  );
});

test("should_flag_the_same_accession_twice_without_a_repository_but_not_once_with_one", () => {
  // given
  const metadata = {
    ...emptyEntryMetadataDraft(),
    method: "X-ray crystallography",
    relatedDatasets: [
      draft({ key: "a", doi: "", accession: "EMPIAR-10028", repository: "" }),
      draft({ key: "b", doi: "", accession: "EMPIAR-10028", repository: "EMPIAR" }),
      draft({ key: "c", doi: "", accession: " empiar-10028 ", repository: "  " }),
    ],
  };

  // when
  const messages = entryFormMessages("Lysozyme", metadata, "").filter((message) =>
    message.field.startsWith("relatedDataset."),
  );

  // then
  assert.deepEqual(
    messages.map((message) => [message.field, message.item, message.message]),
    [["relatedDataset.accession", "3", "this dataset is already listed"]],
  );
});

test("should_place_api_errors_about_accession_and_landing_page_on_their_fields", () => {
  // when / then
  assert.equal(fieldForPointer("/entry/related_datasets/0/accession"), "relatedDataset.accession");
  assert.equal(fieldForPointer("/entry/related_datasets/2/landing_url"), "relatedDataset.landingUrl");
});

test("should_restore_a_saved_dataset_from_before_accessions_with_both_new_values_empty", () => {
  // given
  const store = new Map();
  globalThis.window = {
    localStorage: {
      getItem: (key) => (store.has(key) ? store.get(key) : null),
      setItem: (key, value) => store.set(key, String(value)),
      removeItem: (key) => store.delete(key),
    },
  };
  const savedRow = {
    key: "row-1",
    doi: "10.15785/SBGRID/1303",
    dataType: "diffraction image data",
    repository: "",
    details: "",
  };
  window.localStorage.setItem(
    DRAFT_STORAGE_KEY,
    JSON.stringify({
      version: 5,
      entryId: "e-1",
      title: "Lysozyme",
      thumbUrl: null,
      thumbUploadStatus: "idle",
      files: [],
      metadata: { ...emptyEntryMetadataDraft(), relatedDatasets: [savedRow] },
    }),
  );

  try {
    // when
    const restored = drafts.readStoredDraft();

    // then
    assert.deepEqual(restored.metadata.relatedDatasets, [{ ...savedRow, accession: "", landingUrl: "" }]);
    assert.equal(relatedDatasetsRequest(restored.metadata.relatedDatasets)[0].accession, undefined);
  } finally {
    delete globalThis.window;
  }
});

test("should_link_an_accession_to_its_landing_page_and_a_doi_to_its_resolver", () => {
  // given
  const datasets = [
    {
      accession: "EMPIAR-10028",
      landing_url: "https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
      data_type: "EM raw micrographs",
      repository: "EMPIAR",
    },
    { accession: "1303", data_type: "diffraction image data", repository: "SBGrid Data Bank" },
    {
      doi: "10.15785/SBGRID/1304",
      accession: "1304",
      landing_url: "https://data.sbgrid.org/dataset/1304/",
      data_type: "diffraction image data",
      repository: "SBGrid Data Bank",
    },
    { accession: "X-1", landing_url: "javascript:alert(1)", data_type: "other data" },
  ];

  // when
  const html = renderToStaticMarkup(React.createElement(RelatedDatasets, { datasets }));

  // then
  assert.ok(html.includes('href="https://www.ebi.ac.uk/empiar/EMPIAR-10028/"'));
  assert.ok(html.includes(">EMPIAR-10028</a>"));
  assert.ok(html.includes(">1303</span>"));
  assert.ok(html.includes('href="https://doi.org/10.15785/SBGRID/1304"'));
  assert.ok(html.includes("diffraction image data · SBGrid Data Bank · 1304"));
  assert.ok(html.includes('href="https://data.sbgrid.org/dataset/1304/"'));
  assert.ok(html.includes(">Landing page</a>"));
  assert.ok(!html.includes("javascript:"));
  assert.ok(html.includes(">X-1</span>"));
});

test("should_show_one_review_row_per_accession_within_its_repository", () => {
  // given
  const active = {
    ...storedEntry({
      related_datasets: [{ accession: "EMPIAR-10028", data_type: "EM raw micrographs", repository: "EMPIAR" }],
    }),
    protein_sequences: [],
    artifacts: [],
  };
  const proposed = {
    ...active,
    related_datasets: [
      {
        accession: "empiar-10028",
        landing_url: "https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
        data_type: "EM raw micrographs",
        repository: "EMPIAR",
      },
      { accession: "EMPIAR-10028", data_type: "EM raw micrographs", repository: "Zenodo" },
    ],
  };

  // when
  const section = diffEntry(active, proposed).find((item) => item.title === "Related datasets");

  // then
  assert.deepEqual(
    section.rows.map((row) => [row.label, row.state]),
    [
      ["empiar-10028", "changed"],
      ["EMPIAR-10028", "added"],
    ],
  );
});
