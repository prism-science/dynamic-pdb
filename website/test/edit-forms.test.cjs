require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  archiveLockLabel,
  buildEntryRevisionChange,
  buildModelRevisionChange,
  entryArchiveLocks,
  entryDraftFromEntry,
  hasChanges,
  modelDraftFromModel,
  modelLocks,
} = require("../src/app/components/entry-form/revisionChanges.ts");

// An entry as GET /v1/entries/{id} returns it, through entryFromDocument.
function storedEntry(overrides = {}) {
  return {
    id: "dpdb_entry1",
    created_by: "user-1",
    title: "Lysozyme at room temperature",
    thumbnail_image_url: null,
    external_refs: { pdb: "7L0B", emdb: "EMD-1234" },
    details: "Collected at 293 K.",
    method: "X-ray crystallography",
    resolution: 1.77,
    space_group: "P 43 21 2",
    crystallography: {
      crystals: [
        {
          id: "1",
          growth: { ph: 4.5, temperature_kelvin: 293, method: "vapour diffusion" },
          diffractions: [{ id: "1", temperature_kelvin: 100, detector: "PILATUS" }],
        },
      ],
    },
    ligands: ["NAG"],
    cofactors: [],
    polymer_entities: [
      {
        id: "pe-1",
        label_entity_id: "1",
        description: "Lysozyme C",
        source_organisms: [{ scientific_name: "Gallus gallus", ncbi_taxonomy_id: 9031 }],
        construct: "residues 19-147",
        mutations: [],
        uniprot_mappings: [{ accession: "P00698", source: "sifts" }],
        created_at: "2026-10-01T00:00:00Z",
      },
    ],
    depositor_polymer_entities: [],
    field_sources: {},
    published_at: "2026-10-01T00:00:00Z",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

function editedDraft(entry, edit) {
  const draft = entryDraftFromEntry(entry);
  edit(draft);
  return draft;
}

// A model as GET /v1/entries/{id}/models/{id} returns it.
function storedModel(overrides = {}) {
  return {
    id: "dpdb_entry1_m_001",
    entry_id: "dpdb_entry1",
    created_by: "user-1",
    title: "Refined with qFit",
    thumbnail_image_url: null,
    primary_artifact_id: "artifact-1",
    metadata: {
      purpose: "Refinement",
      model_type: "Multiconformer",
      details: "qFit after phenix.refine.",
      authors: ["A. Author"],
      affiliation: "Astera",
      atom_count: 1234,
      ligands: ["NAG"],
      cofactors: [],
      extensions: { lab_notebook: "page 12" },
    },
    residue_data: [{ label_asym_id: "A", label_seq_id: 1, rscc: 0.9 }],
    metrics: [
      {
        id: "metric-1",
        key: "r_free",
        value: 0.231,
        software_name: "phenix.refine",
        software_version: "1.21",
        run_id: "run-1",
        created_at: "2026-10-01T00:00:00Z",
      },
      {
        id: "metric-2",
        key: "r_work",
        value: 0.196,
        software_name: "phenix.refine",
        software_version: "1.21",
        run_id: "run-1",
        created_at: "2026-10-01T00:00:00Z",
      },
    ],
    field_sources: {},
    published_at: "2026-10-01T00:00:00Z",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

function editedModelDraft(model, edit) {
  const draft = modelDraftFromModel(model);
  edit(draft);
  return draft;
}

function counter() {
  let next = 0;
  return () => `new-id-${++next}`;
}

// ------------------------------------------------------------ entry --

test("should_send_no_keys_when_nothing_changed", () => {
  const entry = storedEntry();

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    entryDraftFromEntry(entry),
  );

  assert.deepEqual(change, {});
  assert.equal(hasChanges(change), false);
});

test("should_send_only_method_when_only_method_changed", () => {
  const entry = storedEntry();

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.method = "Neutron diffraction";
    }),
  );

  assert.deepEqual(change, { method: "Neutron diffraction" });
});

test("should_send_null_when_a_value_is_cleared", () => {
  const entry = storedEntry();

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.spaceGroup = " ";
    }),
  );

  assert.deepEqual(change, { space_group: null });
});

test("should_keep_other_external_refs_when_a_pdb_id_is_added", () => {
  const entry = storedEntry({ external_refs: { emdb: "EMD-1234" } });

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.pdb = "8abc";
    }),
  );

  assert.deepEqual(change, { external_refs: { emdb: "EMD-1234", pdb: "8ABC" } });
});

test("should_never_change_the_pdb_id_when_the_entry_already_has_one", () => {
  const entry = storedEntry();

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.pdb = "9XYZ";
    }),
  );

  assert.deepEqual(change, {});
});

test("should_keep_unknown_crystal_keys_when_a_crystal_value_changes", () => {
  const entry = storedEntry();

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.crystals[0].ph = "5";
    }),
  );

  assert.deepEqual(change, {
    crystallography: {
      crystals: [
        {
          id: "1",
          growth: { ph: 5, temperature_kelvin: 293, method: "vapour diffusion" },
          diffractions: [{ id: "1", temperature_kelvin: 100, detector: "PILATUS" }],
        },
      ],
    },
  });
});

test("should_give_a_new_crystal_the_next_free_ids_when_one_is_added", () => {
  const entry = storedEntry();

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.crystals.push({
        key: "new-crystal",
        ph: "",
        growthTemperature: "",
        diffractions: [{ key: "new-diffraction", temperature: "100" }],
      });
    }),
  );

  assert.deepEqual(change.crystallography.crystals[1], {
    id: "2",
    diffractions: [{ id: "2", temperature_kelvin: 100 }],
  });
  assert.equal(change.crystallography.crystals[0].growth.method, "vapour diffusion");
});

test("should_keep_typed_values_of_hidden_entities_when_a_shown_entity_changes", () => {
  const entry = storedEntry({
    depositor_polymer_entities: [
      { label_entity_id: "1", construct: "residues 19-147, His tag" },
      { label_entity_id: "9", mutations: ["C54T"] },
    ],
  });

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.polymerEntities[0].mutations = ["D52N"];
    }),
  );

  assert.deepEqual(change, {
    polymer_entities: [
      { label_entity_id: "9", mutations: ["C54T"] },
      { label_entity_id: "1", construct: "residues 19-147, His tag", mutations: ["D52N"] },
    ],
  });
});

test("should_never_send_rcsb_fields_when_the_draft_changes_them", () => {
  const entry = storedEntry({
    field_sources: {
      "/title": { source: "rcsb", accession: "7L0B", release: "4.3" },
      "/metadata/method": { source: "depositor" },
    },
  });

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.title = "A depositor title";
      draft.metadata.method = "Neutron diffraction";
    }),
  );

  assert.deepEqual(change, { method: "Neutron diffraction" });
});

test("should_lock_only_rcsb_fields_when_sources_mix_rcsb_depositor_and_none", () => {
  const entry = storedEntry({
    field_sources: {
      "/title": { source: "rcsb", accession: "7L0B", release: "4.3" },
      "/metadata/method": { source: "depositor" },
    },
  });

  const locks = entryArchiveLocks(entry);

  assert.deepEqual(locks, { title: { accession: "7L0B", release: "4.3" } });
  assert.equal(archiveLockLabel(locks.title), "from PDB 7L0B · archive 4.3");
});

test("should_lock_every_archive_field_when_sources_are_null_and_entry_has_a_pdb_id", () => {
  const locks = entryArchiveLocks(storedEntry({ field_sources: null }));

  assert.deepEqual(Object.keys(locks).sort(), [
    "crystallography",
    "details",
    "method",
    "resolution",
    "space_group",
    "title",
  ]);
  assert.equal(archiveLockLabel(locks.method), "from PDB 7L0B");
});

test("should_lock_nothing_when_sources_are_null_and_entry_has_no_pdb_id", () => {
  const locks = entryArchiveLocks(
    storedEntry({ field_sources: null, external_refs: { emdb: "EMD-1234" } }),
  );

  assert.deepEqual(locks, {});
});

test("should_fail_when_a_field_source_kind_is_unknown", () => {
  const entry = storedEntry({ field_sources: { "/title": { source: "guess" } } });

  assert.throws(() => entryArchiveLocks(entry), /unknown field source: guess/);
});

// ------------------------------------------------------------ model --

test("should_send_no_keys_when_a_model_is_unchanged", () => {
  const model = storedModel();

  const change = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    modelDraftFromModel(model),
  );

  assert.deepEqual(change, {});
});

test("should_keep_unseen_metadata_keys_when_the_description_changes", () => {
  const model = storedModel();

  const change = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    editedModelDraft(model, (draft) => {
      draft.details = "qFit after a second round of refinement.";
    }),
  );

  assert.deepEqual(change, {
    metadata: {
      ...model.metadata,
      details: "qFit after a second round of refinement.",
    },
  });
  assert.equal("residue_data" in change.metadata, false);
});

test("should_send_no_purpose_or_type_when_the_model_is_deposited_or_reference", () => {
  const model = storedModel({
    metadata: {
      purpose: "Reference",
      model_type: "Deposited",
      authors: ["PDB"],
      external_refs: { pdb: "7L0B" },
    },
    field_sources: null,
  });

  const locks = modelLocks(model);
  const change = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    editedModelDraft(model, (draft) => {
      draft.purpose = "Refinement";
      draft.modelType = "Multiconformer";
    }),
  );

  assert.equal(locks.purpose, true);
  assert.equal(locks.modelType, true);
  assert.deepEqual(change, {});
});

test("should_lock_archive_fields_of_a_deposited_model_when_sources_are_null", () => {
  const deposited = storedModel({
    metadata: {
      purpose: "Reference",
      model_type: "Deposited",
      authors: ["PDB"],
      external_refs: { pdb: "7L0B" },
    },
    field_sources: null,
  });
  const computed = storedModel({ field_sources: null });

  assert.deepEqual(Object.keys(modelLocks(deposited).fields).sort(), [
    "affiliation",
    "authors",
    "details",
    "ligands",
  ]);
  assert.deepEqual(modelLocks(computed).fields, {});
  assert.deepEqual(modelLocks(computed).metrics, {});
});

test("should_send_metrics_only_when_one_changed_with_fresh_ids_and_their_runs", () => {
  const model = storedModel();

  const unchanged = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    editedModelDraft(model, (draft) => {
      draft.title = "Refined with qFit, round 2";
    }),
    counter(),
  );
  const changed = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    editedModelDraft(model, (draft) => {
      draft.metrics[0].values.r_free = "0.229";
    }),
    counter(),
  );

  assert.deepEqual(unchanged, { title: "Refined with qFit, round 2" });
  assert.deepEqual(changed, {
    metrics: [
      {
        id: "new-id-1",
        key: "r_work",
        value: 0.196,
        software_name: "phenix.refine",
        software_version: "1.21",
        run_id: "run-1",
      },
      {
        id: "new-id-2",
        key: "r_free",
        value: 0.229,
        software_name: "phenix.refine",
        software_version: "1.21",
        run_id: "run-1",
      },
    ],
  });
});

test("should_keep_a_locked_metric_at_its_stored_value_when_metrics_are_sent", () => {
  const model = storedModel({
    metadata: { purpose: "Reference", model_type: "Deposited", external_refs: { pdb: "7L0B" } },
    field_sources: {
      "/metrics/r_free": { source: "rcsb", accession: "7L0B", release: "4.3" },
    },
  });

  const change = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    editedModelDraft(model, (draft) => {
      draft.metrics[0].values.r_free = "0.5";
      draft.metrics[0].values.r_work = "0.2";
    }),
    counter(),
  );

  assert.deepEqual(
    change.metrics.map((metric) => [metric.key, metric.value]),
    [
      ["r_free", 0.231],
      ["r_work", 0.2],
    ],
  );
});

test("should_lock_only_the_archive_metrics_when_a_deposited_model_has_null_sources", () => {
  const model = storedModel({
    metadata: { purpose: "Reference", model_type: "Deposited", external_refs: { pdb: "7L0B" } },
    field_sources: null,
  });

  assert.deepEqual(Object.keys(modelLocks(model).metrics).sort(), [
    "clashscore",
    "r_free",
    "r_work",
    "ramachandran_outliers",
  ]);
});

test("should_send_a_new_score_and_keep_the_archive_ones_when_a_deposited_model_gets_one", () => {
  const model = storedModel({
    metadata: { purpose: "Reference", model_type: "Deposited", external_refs: { pdb: "7L0B" } },
    field_sources: null,
  });

  const change = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    editedModelDraft(model, (draft) => {
      draft.metrics[0].values.rscc = "0.91";
    }),
    counter(),
  );

  assert.deepEqual(
    change.metrics.map((metric) => [metric.key, metric.value, metric.run_id]),
    [
      ["r_free", 0.231, "run-1"],
      ["r_work", 0.196, "run-1"],
      ["rscc", 0.91, "run-1"],
    ],
  );
});

test("should_lock_nothing_when_a_deposited_model_names_no_pdb_id", () => {
  const model = storedModel({
    metadata: { purpose: "Reference", model_type: "Deposited" },
    field_sources: null,
  });

  const locks = modelLocks(model);

  assert.deepEqual(locks.fields, {});
  assert.deepEqual(locks.metrics, {});
});

test("should_keep_organisms_the_form_does_not_show_when_a_shown_entity_changes", () => {
  const typedOrganisms = [
    { scientific_name: "Escherichia coli", ncbi_taxonomy_id: 562 },
    { scientific_name: "Homo sapiens", ncbi_taxonomy_id: 9606 },
  ];
  const entry = storedEntry({
    polymer_entities: [
      {
        id: "pe-1",
        label_entity_id: "1",
        description: "Chimera",
        source_organisms: typedOrganisms,
        mutations: [],
        uniprot_mappings: [],
        created_at: "2026-10-01T00:00:00Z",
      },
    ],
    depositor_polymer_entities: [{ label_entity_id: "1", source_organisms: typedOrganisms }],
  });

  const change = buildEntryRevisionChange(
    entry,
    entryDraftFromEntry(entry),
    editedDraft(entry, (draft) => {
      draft.metadata.polymerEntities[0].mutations = ["D52N"];
    }),
  );

  assert.deepEqual(change, {
    polymer_entities: [
      { label_entity_id: "1", mutations: ["D52N"], source_organisms: typedOrganisms },
    ],
  });
});

test("should_keep_a_metric_the_form_has_no_field_for_when_metrics_are_sent", () => {
  const model = storedModel();
  model.metrics.push({
    id: "metric-3",
    key: "custom_score",
    value: 7,
    software_name: "lab script",
    software_version: null,
    run_id: null,
    created_at: "2026-10-01T00:00:00Z",
  });

  const change = buildModelRevisionChange(
    model,
    modelDraftFromModel(model),
    editedModelDraft(model, (draft) => {
      draft.metrics[0].values.r_free = "0.229";
    }),
    counter(),
  );

  assert.deepEqual(change.metrics[0], {
    id: "new-id-1",
    key: "custom_score",
    value: 7,
    software_name: "lab script",
    software_version: null,
    run_id: null,
  });
  assert.deepEqual(
    change.metrics.map((metric) => metric.key),
    ["custom_score", "r_work", "r_free"],
  );
});
