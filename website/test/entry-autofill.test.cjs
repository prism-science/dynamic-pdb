require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  archiveAutofill,
  autofilledMetadata,
  autofilledTitle,
  withAutofilledRequest,
} = require("../src/app/components/entry-form/autofill.ts");
const { emptyEntryMetadataDraft } = require("../src/app/components/entry-form/types.ts");

// An entry proposal as GET /v1/archive/pdb/{id} returns it.
function proposal(overrides = {}) {
  return {
    title: "Lysozyme at room temperature",
    resolution: 1.77,
    method: "X-ray crystallography",
    space_group: "P 43 21 2",
    crystallography: {
      crystals: [
        { id: "1", growth: { ph: 4.5 }, diffractions: [{ id: "1", temperature_kelvin: 100 }] },
        { id: "2" },
      ],
    },
    latest_version: "1.2",
    revision_groups: [],
    supersedes: [],
    superseded_by: [],
    provenance: {},
    conflicts: [],
    warnings: [],
    ...overrides,
  };
}

function typedMetadata() {
  return {
    ...emptyEntryMetadataDraft(),
    pdb: "7L0B",
    details: "Typed description",
    method: "Typed method",
    resolution: "2.5",
  };
}

test("should_lock_each_value_with_its_pdb_id_and_release_when_the_archive_has_it", () => {
  const autofill = archiveAutofill("7L0B", proposal());

  assert.deepEqual(Object.keys(autofill.locks).sort(), [
    "crystallography",
    "method",
    "resolution",
    "space_group",
    "title",
  ]);
  assert.deepEqual(autofill.locks.title, { accession: "7L0B", release: "1.2" });
});

test("should_leave_a_value_editable_when_the_archive_does_not_have_it", () => {
  const autofill = archiveAutofill("7L0B", proposal({ title: null, crystallography: { crystals: [] } }));
  const typed = typedMetadata();

  assert.equal(autofill.locks.title, undefined);
  assert.equal(autofill.locks.details, undefined);
  assert.equal(autofill.locks.crystallography, undefined);
  assert.equal(autofilledTitle("Typed title", autofill), "Typed title");
  assert.equal(autofilledMetadata(typed, autofill).details, "Typed description");
});

test("should_show_the_archive_values_over_the_typed_ones_when_the_lookup_has_finished", () => {
  const shown = autofilledMetadata(typedMetadata(), archiveAutofill("7L0B", proposal()));

  assert.equal(shown.method, "X-ray crystallography");
  assert.equal(shown.resolution, "1.77");
  assert.equal(shown.spaceGroup, "P 43 21 2");
  assert.equal(shown.details, "Typed description");
  assert.deepEqual(
    shown.crystals.map((crystal) => [crystal.ph, crystal.diffractions.length]),
    [
      ["4.5", 1],
      ["", 0],
    ],
  );
});

test("should_give_the_typed_values_back_when_the_pdb_id_is_cleared", () => {
  const typed = typedMetadata();

  assert.equal(autofilledTitle("Typed title", null), "Typed title");
  assert.equal(autofilledMetadata(typed, null), typed);
});

test("should_send_the_archive_values_exactly_as_returned_when_the_form_was_autofilled", () => {
  const archive = proposal();
  const input = {
    title: "Lysozyme at room temperature",
    method: "X-ray crystallography",
    resolution: 1.77,
    // The form numbers crystals itself; the archive's ids and keys must win.
    crystallography: { crystals: [{ id: "1", growth: { ph: 4.5 } }, { id: "2" }] },
    external_refs: { pdb: "7L0B" },
    entities: [],
  };

  const request = withAutofilledRequest(input, archiveAutofill("7L0B", archive));

  assert.deepEqual(request.crystallography, archive.crystallography);
  assert.equal(request.space_group, "P 43 21 2");
  assert.deepEqual(request.external_refs, { pdb: "7L0B" });
  assert.deepEqual(request.entities, []);
});
