require("./register.cjs");

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const {
  createEntryRequestBody,
  createEntryRevisionRequestBody,
  createModelRevisionRequestBody,
} = require("../src/lib/api/entries.ts");
const {
  archiveAutofill,
  autofilledMetadata,
  autofilledTitle,
  withAutofilledRequest,
} = require("../src/app/components/entry-form/autofill.ts");
const { buildCreateEntryInput } = require("../src/app/components/entry-form/helpers.ts");
const { polymerEntityDraftsFromArchive } = require("../src/app/components/entry-form/polymerEntities.ts");
const {
  buildEntryRevisionChange,
  buildModelRevisionChange,
  entryDraftFromEntry,
  modelDraftFromModel,
} = require("../src/app/components/entry-form/revisionChanges.ts");
const { emptyEntryMetadataDraft } = require("../src/app/components/entry-form/types.ts");

// The requests the forms send for fixed drafts, kept in a committed golden
// file. integration/form_parity_test.go posts each one, and a hand-written API
// body with the same content, and checks both store the same records. A form
// change that alters a request fails here first.
//
// After an intended change, rewrite the file with:
//   UPDATE_FORM_PARITY=1 node --test test/form-parity.test.cjs
const GOLDEN_PATH = path.join(__dirname, "fixtures", "form-parity-requests.json");

// The integration test serves this PDB ID from its RCSB stub, under an
// unused ID it puts in place of this one.
const PDB_ID = "9PAR";

// What the RCSB stub of the integration tests proposes for an entry.
const STUB_PROPOSAL = {
  title: "example structure",
  resolution: 1.4,
  method: "X-ray crystallography",
  space_group: "P 21 21 21",
  latest_version: "1.1",
  revision_groups: [],
  supersedes: [],
  superseded_by: [],
  provenance: {},
  conflicts: [],
  warnings: [],
};
const STUB_POLYMER_ENTITIES = [
  {
    label_entity_id: "1",
    source_organisms: [{ scientific_name: "Homo sapiens" }],
    uniprot_mappings: [],
    provenance: {},
    conflicts: [],
    warnings: [],
  },
];

// The base entry the integration test creates, as GET /v1/entries/{id} returns it.
const BASE_ENTRY = {
  id: "parity-entry",
  created_by: "parity-user",
  title: "Parity base entry",
  thumbnail_image_url: null,
  method: "X-ray crystallography",
  resolution: 2,
  crystallography: {
    crystals: [{ id: "1", growth: { ph: 7 }, diffractions: [{ id: "1", temperature_kelvin: 100 }] }],
  },
  ligands: ["ATP"],
  cofactors: [],
  polymer_entities: [],
  depositor_polymer_entities: [],
  field_sources: {},
  published_at: null,
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
};

// The base model the integration test seeds, as GET .../models/{id} returns it.
const BASE_MODEL = {
  id: "parity-entry_m_001",
  entry_id: "parity-entry",
  created_by: "parity-user",
  title: "Parity base model",
  thumbnail_image_url: null,
  primary_artifact_id: null,
  metadata: {
    purpose: "Refinement",
    model_type: "Multiconformer",
    details: "Base details",
    authors: ["A. Author"],
    ligands: [],
    cofactors: [],
    extensions: { lab: { notebook: "page 12" } },
  },
  metrics: [],
  field_sources: {},
  published_at: null,
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
};

function entryCreateRequest() {
  // The depositor typed these; the PDB lookup then filled title, method,
  // resolution and space group, which the form shows read-only.
  const typed = {
    ...emptyEntryMetadataDraft(),
    pdb: PDB_ID,
    details: "Room-temperature structure.",
    method: "a method the archive replaces",
    compounds: {
      ligands: { mode: "list", items: [{ id: "ATP", review: null }] },
      cofactors: { mode: "none", items: [] },
    },
    crystals: [
      {
        key: "typed-crystal",
        ph: "7.5",
        growthTemperature: "293",
        diffractions: [{ key: "typed-diffraction", temperature: "100" }],
      },
    ],
    polymerEntities: polymerEntityDraftsFromArchive(STUB_POLYMER_ENTITIES, [
      {
        labelEntityId: "1",
        construct: "residues 1-164",
        mutations: ["C54T"],
        organismName: "",
        organismTaxonomyId: "",
      },
    ]),
  };
  const autofill = archiveAutofill(PDB_ID, STUB_PROPOSAL);
  const input = withAutofilledRequest(
    buildCreateEntryInput(
      autofilledTitle("A title the archive replaces", autofill),
      autofilledMetadata(typed, autofill),
      null,
      [],
    ),
    autofill,
  );
  return createEntryRequestBody(input);
}

function entryRevisionRequest() {
  const initial = entryDraftFromEntry(BASE_ENTRY);
  const current = entryDraftFromEntry(BASE_ENTRY);
  current.metadata.method = "Neutron diffraction";
  current.metadata.crystals[0].ph = "7.5";
  current.metadata.crystals.push({
    key: "added-crystal",
    ph: "",
    growthTemperature: "293",
    diffractions: [],
  });
  return createEntryRevisionRequestBody(buildEntryRevisionChange(BASE_ENTRY, initial, current));
}

function modelRevisionRequest() {
  const initial = modelDraftFromModel(BASE_MODEL);
  const current = modelDraftFromModel(BASE_MODEL);
  current.details = "Edited details";
  current.authors = ["A. Author", "B. Author"];
  current.metrics = [
    {
      id: "added-metrics",
      values: { r_free: "0.231" },
      softwareName: "phenix.refine",
      softwareVersion: "1.21",
    },
  ];
  // The integration test gives every metric a fresh id before it posts.
  let next = 0;
  const placeholderId = () => `00000000-0000-4000-8000-${String(++next).padStart(12, "0")}`;
  return createModelRevisionRequestBody(
    buildModelRevisionChange(BASE_MODEL, initial, current, placeholderId),
  );
}

function formRequests() {
  return {
    entry_create: entryCreateRequest(),
    entry_revision: entryRevisionRequest(),
    model_revision: modelRevisionRequest(),
  };
}

test("should_send_the_requests_in_the_golden_file_when_the_forms_have_fixed_drafts", () => {
  const actual = formRequests();
  if (process.env.UPDATE_FORM_PARITY === "1") {
    fs.writeFileSync(GOLDEN_PATH, `${JSON.stringify(actual, null, 2)}\n`);
  }

  const golden = JSON.parse(fs.readFileSync(GOLDEN_PATH, "utf8"));

  assert.deepEqual(
    JSON.parse(JSON.stringify(actual)),
    golden,
    "a form request changed; if that is intended, run UPDATE_FORM_PARITY=1 node --test test/form-parity.test.cjs and update integration/form_parity_test.go to match",
  );
});

test("should_send_the_archive_values_when_the_create_form_was_autofilled", () => {
  const entry = entryCreateRequest().entry;

  assert.equal(entry.title, STUB_PROPOSAL.title);
  assert.equal(entry.method, STUB_PROPOSAL.method);
  assert.equal(entry.resolution, STUB_PROPOSAL.resolution);
  assert.equal(entry.space_group, STUB_PROPOSAL.space_group);
  assert.equal(entry.details, "Room-temperature structure.");
});
