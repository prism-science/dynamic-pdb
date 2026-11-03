require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const choices = require("../src/app/components/entry-form/archiveChoices.ts");
const {
  archiveAutofill,
  autofilledMetadata,
  autofilledTitle,
  withAutofilledRequest,
} = require("../src/app/components/entry-form/autofill.ts");
const drafts = require("../src/app/components/entry-form/drafts.ts");
const {
  buildEntryRevisionChange,
  entryArchiveLocks,
  entryDraftFromEntry,
} = require("../src/app/components/entry-form/revisionChanges.ts");
const { DRAFT_VERSION, emptyEntryMetadataDraft } = require("../src/app/components/entry-form/types.ts");

// An entry proposal as GET /v1/archive/pdb/{id} returns it.
function proposal(overrides = {}) {
  return {
    title: "Lysozyme at room temperature",
    details: "Collected at 293 K.",
    resolution: 1.77,
    method: "X-ray crystallography",
    space_group: "P 43 21 2",
    crystallography: {
      crystals: [{ id: "1", growth: { ph: 4.5 }, diffractions: [{ id: "1", temperature_kelvin: 100 }] }],
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

// What the New entry form holds: the typed title and metadata.
function typedForm(title = "", metadata = {}) {
  return { title, metadata: { ...emptyEntryMetadataDraft(), pdb: "7L0B", ...metadata } };
}

function newEntryView(form, own, archiveProposal = proposal()) {
  const autofill = archiveAutofill("7L0B", archiveProposal);
  return choices.archiveFieldsView(
    autofill.locks,
    choices.archiveValuesFromAutofill(autofill),
    choices.archiveFieldValues(form),
    own,
    true,
  );
}

// An entry as GET /v1/entries/{id} returns it, through entryFromDocument.
function storedEntry(overrides = {}) {
  return {
    id: "dpdb_entry1",
    created_by: "user-1",
    title: "Lysozyme at room temperature",
    thumbnail_image_url: null,
    external_refs: { pdb: "7L0B" },
    details: "Collected at 293 K.",
    method: "X-ray crystallography",
    resolution: 1.77,
    space_group: "P 43 21 2",
    crystallography: { crystals: [{ id: "1", growth: { ph: 4.5 } }] },
    ligands: ["NAG"],
    cofactors: [],
    polymer_entities: [],
    depositor_polymer_entities: [],
    field_sources: {
      "/title": { source: "depositor" },
      "/metadata/resolution": { source: "rcsb", accession: "7L0B", release: "4.3" },
      "/metadata/ligands": { source: "rcsb", accession: "7L0B", release: "4.3" },
    },
    published_at: "2026-10-01T00:00:00Z",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

function editDraft(entry, edit) {
  const draft = entryDraftFromEntry(entry);
  edit(draft);
  return draft;
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

function storedDraftJSON(overrides) {
  return JSON.stringify({
    entryId: "e-1",
    title: "Typed title",
    thumbUrl: null,
    thumbUploadStatus: "idle",
    files: [],
    metadata: { ...emptyEntryMetadataDraft(), pdb: "7L0B" },
    ...overrides,
  });
}

// ------------------------------------------------------------ field keys --

test("should_accept_only_the_six_archive_fields_when_checking_a_key", () => {
  assert.equal(choices.isEntryArchiveField("space_group"), true);
  assert.equal(choices.isEntryArchiveField("ligands"), false);
  assert.equal(choices.isEntryArchiveField(undefined), false);
});

test("should_keep_known_fields_in_form_order_and_drop_the_rest_when_reading_stored_own_fields", () => {
  assert.deepEqual(
    choices.ownFieldsFromStored(["resolution", "bogus", "title", "title", 7]),
    ["title", "resolution"],
  );
  assert.deepEqual(choices.ownFieldsFromStored(undefined), []);
  assert.deepEqual(choices.ownFieldsFromStored("title"), []);
});

test("should_add_or_remove_one_own_field_and_keep_form_order", () => {
  assert.deepEqual(choices.withOwnField(["resolution"], "title", true), ["title", "resolution"]);
  assert.deepEqual(choices.withOwnField(["title", "resolution"], "title", false), ["resolution"]);
  assert.deepEqual(choices.withOwnField(["title"], "title", true), ["title"]);
  assert.deepEqual(choices.withOwnField([], "title", false), []);
});

test("should_name_the_section_that_shows_each_archive_field", () => {
  assert.equal(choices.archiveFieldSection("title"), "overview");
  assert.equal(choices.archiveFieldSection("details"), "overview");
  assert.equal(choices.archiveFieldSection("resolution"), "experiment");
  assert.equal(choices.archiveFieldSection("crystallography"), "experiment");
});

// ------------------------------------------------------- value equality --

test("should_compare_text_fields_without_surrounding_spaces", () => {
  const a = choices.archiveFieldValues(typedForm(" Lysozyme ", { method: "CryoEM", spaceGroup: "P 1" }));
  const b = choices.archiveFieldValues(typedForm("Lysozyme", { method: "CryoEM ", spaceGroup: "P 21" }));

  assert.equal(choices.sameArchiveValue("title", a, b), true);
  assert.equal(choices.sameArchiveValue("method", a, b), true);
  assert.equal(choices.sameArchiveValue("details", a, b), true);
  assert.equal(choices.sameArchiveValue("space_group", a, b), false);
});

test("should_compare_resolution_as_a_number", () => {
  const a = choices.archiveFieldValues(typedForm("", { resolution: "1.50" }));
  const b = choices.archiveFieldValues(typedForm("", { resolution: "1.5" }));
  const c = choices.archiveFieldValues(typedForm("", { resolution: "1.6" }));

  assert.equal(choices.sameArchiveValue("resolution", a, b), true);
  assert.equal(choices.sameArchiveValue("resolution", a, c), false);
});

test("should_compare_crystals_by_value_and_not_by_the_form_keys", () => {
  const crystal = (key, ph) => ({ key, ph, growthTemperature: "", diffractions: [] });
  const a = choices.archiveFieldValues(typedForm("", { crystals: [crystal("k-1", "4.5")] }));
  const b = choices.archiveFieldValues(typedForm("", { crystals: [crystal("stored-crystal:1", "4.50")] }));
  const c = choices.archiveFieldValues(typedForm("", { crystals: [crystal("k-1", "7")] }));

  assert.equal(choices.sameArchiveValue("crystallography", a, b), true);
  assert.equal(choices.sameArchiveValue("crystallography", a, c), false);
});

test("should_tell_a_blank_value_from_a_set_one_for_each_field", () => {
  const blank = choices.archiveFieldValues(typedForm("  "));
  const set = choices.archiveFieldValues(
    typedForm("T", {
      details: "D",
      method: "M",
      resolution: "2",
      spaceGroup: "P 1",
      crystals: [{ key: "k", ph: "", growthTemperature: "", diffractions: [] }],
    }),
  );

  for (const field of choices.ENTRY_ARCHIVE_FIELDS) {
    assert.equal(choices.isBlankArchiveValue(field, blank), true, field);
    assert.equal(choices.isBlankArchiveValue(field, set), false, field);
  }
});

test("should_fail_loudly_when_a_field_key_is_unknown", () => {
  const values = choices.archiveFieldValues(typedForm());
  const autofill = archiveAutofill("7L0B", proposal());

  assert.throws(() => choices.sameArchiveValue("ligands", values, values), /unknown entry archive field: ligands/);
  assert.throws(() => choices.isBlankArchiveValue("ligands", values), /unknown entry archive field: ligands/);
  assert.throws(() => choices.withoutOwnFields(autofill, ["ligands"]), /unknown entry archive field: ligands/);
});

// --------------------------------------------------- New entry: take over --

test("should_keep_the_typed_value_when_the_depositor_takes_over_a_field_they_typed", () => {
  const form = typedForm("My own title");
  const archive = choices.archiveValuesFromAutofill(archiveAutofill("7L0B", proposal()));

  const taken = choices.takeOver(form, "title", archive);

  assert.equal(taken.title, "My own title");
});

test("should_start_from_the_archive_value_when_the_depositor_takes_over_a_field_they_left_empty", () => {
  const form = typedForm("", { resolution: "" });
  const archive = choices.archiveValuesFromAutofill(archiveAutofill("7L0B", proposal()));

  const resolution = choices.takeOver(form, "resolution", archive);
  const crystals = choices.takeOver(form, "crystallography", archive);

  assert.equal(resolution.metadata.resolution, "1.77");
  assert.equal(resolution.metadata.pdb, "7L0B");
  assert.deepEqual(
    crystals.metadata.crystals.map((crystal) => crystal.ph),
    ["4.5"],
  );
});

test("should_set_only_the_named_field_when_copying_a_value_from_another_source", () => {
  const form = typedForm("Typed", { details: "Typed details", spaceGroup: "P 1" });
  const source = choices.archiveFieldValues(typedForm("Stored", { details: "Stored details", spaceGroup: "C 2" }));

  const reset = choices.withValueFrom(form, "space_group", source);

  assert.equal(reset.metadata.spaceGroup, "C 2");
  assert.equal(reset.metadata.details, "Typed details");
  assert.equal(reset.title, "Typed");
});

test("should_show_check_and_send_the_typed_value_of_an_own_field_and_the_archive_value_of_the_rest", () => {
  const archiveProposal = proposal();
  const autofill = choices.withoutOwnFields(archiveAutofill("7L0B", archiveProposal), ["title"]);
  const form = typedForm("My own title", { resolution: "2.5" });

  const shownTitle = autofilledTitle(form.title, autofill);
  const shown = autofilledMetadata(form.metadata, autofill);
  const request = withAutofilledRequest(
    { title: shownTitle, resolution: 2.5, method: "", external_refs: { pdb: "7L0B" }, entities: [] },
    autofill,
  );

  assert.equal(autofill.locks.title, undefined);
  assert.deepEqual(autofill.locks.resolution, { accession: "7L0B", release: "1.2" });
  assert.equal(shownTitle, "My own title");
  assert.equal(shown.resolution, "1.77");
  assert.equal(request.title, "My own title");
  assert.equal(request.resolution, 1.77);
  assert.deepEqual(request.crystallography, archiveProposal.crystallography);
});

test("should_release_every_field_when_the_depositor_took_all_of_them_over", () => {
  const autofill = choices.withoutOwnFields(
    archiveAutofill("7L0B", proposal()),
    choices.ENTRY_ARCHIVE_FIELDS,
  );

  assert.deepEqual(autofill.locks, {});
  assert.deepEqual(autofill.request, {});
  assert.deepEqual(autofill.metadata, {});
  assert.equal(autofill.title, null);
});

test("should_leave_the_lookup_autofill_unchanged_when_releasing_fields", () => {
  const original = archiveAutofill("7L0B", proposal());

  choices.withoutOwnFields(original, ["title", "resolution"]);

  assert.ok(original.locks.title);
  assert.equal(original.metadata.resolution, "1.77");
});

// ------------------------------------------------ New entry: field states --

test("should_show_a_conflict_when_a_value_typed_before_the_lookup_differs_from_the_archive", () => {
  const view = newEntryView(typedForm("My own title"), []);

  assert.equal(view.states.title.conflict, true);
  assert.equal(view.states.title.own, false);
});

test("should_show_no_conflict_when_the_typed_value_is_empty_equal_or_taken_over", () => {
  const empty = newEntryView(typedForm(""), []);
  const equal = newEntryView(typedForm("Lysozyme at room temperature "), []);
  const own = newEntryView(typedForm("My own title"), ["title"]);

  assert.equal(empty.states.title.conflict, false);
  assert.equal(equal.states.title.conflict, false);
  assert.equal(own.states.title.conflict, false);
});

test("should_show_no_conflict_on_the_edit_form", () => {
  const autofill = archiveAutofill("7L0B", proposal());
  const view = choices.archiveFieldsView(
    autofill.locks,
    choices.archiveValuesFromAutofill(autofill),
    choices.archiveFieldValues(typedForm("Another title")),
    [],
    false,
  );

  assert.equal(view.states.title.conflict, false);
});

test("should_say_an_own_value_stays_the_archives_only_when_it_equals_the_archive_value", () => {
  const same = newEntryView(typedForm("", { resolution: "1.770" }), ["resolution"]);
  const different = newEntryView(typedForm("", { resolution: "2.5" }), ["resolution"]);
  const notOwn = newEntryView(typedForm("", { resolution: "1.77" }), []);

  assert.equal(same.states.resolution.sameAsArchive, true);
  assert.equal(different.states.resolution.sameAsArchive, false);
  assert.equal(notOwn.states.resolution.sameAsArchive, false);
});

test("should_have_no_state_for_a_field_the_archive_does_not_fill", () => {
  const view = newEntryView(typedForm("Typed"), [], proposal({ details: null }));

  assert.equal(view.states.details, undefined);
  assert.ok(view.states.title);
});

test("should_unlock_an_own_field_and_keep_the_others_read_only", () => {
  const view = newEntryView(typedForm(), ["title", "crystallography"]);

  assert.deepEqual(choices.lockedArchiveFields(view.states), [
    "details",
    "method",
    "resolution",
    "space_group",
  ]);
});

test("should_let_an_own_fields_messages_block_and_not_a_read_only_fields", () => {
  const view = newEntryView(typedForm(), ["title", "crystallography"]);

  const locked = choices.lockedFieldIds(view.states);

  assert.equal(locked.has("entry.title"), false);
  assert.equal(locked.has("crystal.ph"), false);
  assert.equal(locked.has("entry.method"), true);
  assert.equal(locked.has("entry.resolution"), true);
});

test("should_badge_an_archive_field_with_its_source_until_taken_over", () => {
  const view = newEntryView(typedForm(), ["title"]);

  assert.equal(choices.archiveFieldBadge(view.states.title), "your value");
  assert.equal(choices.archiveFieldBadge(view.states.method), "from PDB 7L0B · archive 1.2");
});

test("should_name_the_archive_value_by_its_pdb_id_when_it_has_one", () => {
  assert.equal(choices.archiveValueSource({ accession: "7L0B", release: null }), "PDB 7L0B");
  assert.equal(choices.archiveValueSource({ accession: null, release: null }), "The PDB");
});

test("should_list_own_fields_with_a_value_of_their_own_when_a_new_entry_takes_fields_over", () => {
  const view = newEntryView(
    typedForm("My own title", { resolution: "1.77", details: "" }),
    ["title", "resolution", "details"],
  );

  // resolution equals the archive's and details is blank: both stay the archive's.
  assert.deepEqual(choices.newTakenOverFields(view), ["title"]);
  assert.deepEqual(choices.newTakenOverFields(newEntryView(typedForm("My own title"), [])), []);
});

// ------------------------------------------------------------ Edit entry --

test("should_badge_depositor_fields_and_archive_composition_lists", () => {
  const badges = choices.entrySourceBadges(storedEntry());

  assert.deepEqual(badges, {
    title: "set by the depositor",
    ligands: "from PDB 7L0B · archive 4.3",
  });
});

test("should_badge_nothing_when_the_entry_has_no_field_sources", () => {
  assert.deepEqual(choices.entrySourceBadges(storedEntry({ field_sources: null })), {});
});

test("should_fail_loudly_when_a_badge_source_kind_is_unknown", () => {
  const entry = storedEntry({ field_sources: { "/title": { source: "curator" } } });

  assert.throws(() => choices.entrySourceBadges(entry), /unknown field source: curator/);
});

test("should_send_a_changed_value_of_an_own_archive_field_and_drop_the_other_archive_fields", () => {
  const entry = storedEntry();
  const initial = entryDraftFromEntry(entry);
  const current = editDraft(entry, (draft) => {
    draft.metadata.resolution = "2.1";
  });

  assert.deepEqual(buildEntryRevisionChange(entry, initial, current, ["resolution"]), { resolution: 2.1 });
  assert.deepEqual(buildEntryRevisionChange(entry, initial, current, []), {});
  assert.deepEqual(buildEntryRevisionChange(entry, initial, current), {});
});

test("should_send_nothing_when_an_own_archive_field_is_unchanged", () => {
  const entry = storedEntry();
  const draft = entryDraftFromEntry(entry);

  assert.deepEqual(buildEntryRevisionChange(entry, draft, entryDraftFromEntry(entry), ["resolution"]), {});
});

test("should_list_changed_sync_fields_that_were_not_the_depositors_when_an_edit_takes_them_over", () => {
  const entry = storedEntry();

  const fields = choices.editTakenOverFields(entry, {
    title: "Changed",
    resolution: 2.1,
    ligands: ["ATP"],
    details: "Changed details",
    related_datasets: null,
  });

  // title was already the depositor's; details had no source, so the sync could still fill it.
  assert.deepEqual(fields, ["details", "ligands", "resolution"]);
  assert.deepEqual(choices.editTakenOverFields(entry, {}), []);
});

test("should_list_nothing_when_the_edited_entry_has_no_pdb_id", () => {
  const entry = storedEntry({ external_refs: {} });

  assert.deepEqual(choices.editTakenOverFields(entry, { resolution: 2.1 }), []);
});

test("should_list_changed_archive_fields_when_sources_are_null_and_the_entry_has_a_pdb_id", () => {
  const entry = storedEntry({ field_sources: null });

  assert.ok(entryArchiveLocks(entry).title);
  assert.deepEqual(choices.editTakenOverFields(entry, { title: "Changed" }), ["title"]);
});

test("should_name_the_taken_over_fields_in_one_sentence_or_say_nothing", () => {
  assert.equal(choices.takenOverNotice([]), null);
  assert.equal(
    choices.takenOverNotice(["title", "resolution"]),
    "The weekly PDB update will no longer change these fields, because you set them yourself: Title, Resolution.",
  );
});

// ---------------------------------------------------------------- drafts --

test("should_save_and_restore_the_own_fields_with_the_draft", () => {
  withLocalStorage(() => {
    drafts.writeStoredDraft({
      version: DRAFT_VERSION,
      entryId: "e-1",
      title: "My own title",
      thumbUrl: null,
      thumbUploadStatus: "idle",
      files: [],
      metadata: { ...emptyEntryMetadataDraft(), pdb: "7L0B" },
      ownFields: ["title", "crystallography"],
    });

    const restored = drafts.readStoredDraft();

    assert.equal(restored.version, 5);
    assert.equal(restored.title, "My own title");
    assert.deepEqual(restored.ownFields, ["title", "crystallography"]);
  });
});

test("should_restore_a_version_4_or_3_draft_with_no_field_taken_over", () => {
  for (const version of [4, 3]) {
    withLocalStorage(() => {
      window.localStorage.setItem("dpdb:new-entry-draft", storedDraftJSON({ version }));

      const restored = drafts.readStoredDraft();

      assert.equal(restored.version, DRAFT_VERSION, `version ${version}`);
      assert.equal(restored.title, "Typed title");
      assert.deepEqual(restored.ownFields, []);
    });
  }
});

test("should_drop_unknown_own_fields_from_a_saved_draft", () => {
  withLocalStorage(() => {
    window.localStorage.setItem(
      "dpdb:new-entry-draft",
      storedDraftJSON({ version: 5, ownFields: ["title", "ligands", null] }),
    );

    assert.deepEqual(drafts.readStoredDraft().ownFields, ["title"]);
  });
});

test("should_ignore_a_draft_of_an_unknown_version", () => {
  withLocalStorage(() => {
    window.localStorage.setItem("dpdb:new-entry-draft", storedDraftJSON({ version: 2 }));
    assert.equal(drafts.readStoredDraft(), null);

    window.localStorage.setItem("dpdb:new-entry-draft", storedDraftJSON({ version: 6 }));
    assert.equal(drafts.readStoredDraft(), null);
  });
});
