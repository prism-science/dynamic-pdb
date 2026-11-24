require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  addWholeSearch,
  clearedSelection,
  countLines,
  downloadInProgress,
  downloadView,
  emptySelection,
  FILE_CHOICES,
  fileChoiceText,
  formatDecimalBytes,
  isEmptySelection,
  isModelChecked,
  limitMessage,
  MAX_LEFT_OUT_MODELS,
  MAX_PICKED_MODELS,
  MAX_WHOLE_SEARCHES,
  parseStoredSelection,
  problemMessage,
  removeLeftOut,
  removePicked,
  removeWholeSearch,
  REUSE_LICENSE_URL,
  REUSE_TERMS,
  reusableSelectionId,
  selectionFingerprint,
  selectionRequest,
  selectionSize,
  selectionSizeText,
  serializeSelection,
  setModelChecked,
  wholeSearchFromQuery,
  wholeSearchLabel,
  withAddDepositedModels,
  withDownload,
  withFrozen,
  withRetry,
  previewSummary,
  sizeText,
  reasonCodeText,
  artifactTypeLabel,
  availabilityText,
} = require("../src/lib/download-selection.ts");
const { readModelSearch } = require("../src/lib/model-search.ts");
const {
  artifactTypeValues,
  downloadFileAvailabilityValues,
  downloadFileChoiceValues,
  downloadReasonCodeValues,
  downloadSizeKindValues,
  downloadStatusValues,
} = require("../src/lib/api/schema.gen.ts");

function model(id, entryId = "dpdb_entry1") {
  return { id, title: `Model ${id}`, entryId, entryLabel: `${entryId} label` };
}

function changed(change) {
  assert.ok("selection" in change, `expected a selection, got ${JSON.stringify(change)}`);
  return change.selection;
}

const ensembles = wholeSearchFromQuery("model_type=Ensemble", 120, true);

test("should pick a model when its box is checked outside a whole search", () => {
  // given
  const selection = emptySelection();

  // when
  const next = changed(setModelChecked(selection, model("m1"), true, "organism=Homo+sapiens"));

  // then
  assert.deepEqual(next.picked, [model("m1")]);
  assert.equal(isModelChecked(next, "m1", null), true);
  assert.equal(isModelChecked(next, "m1", "model_type=Ensemble"), true);
});

test("should remove a picked model when its box is unchecked outside a whole search", () => {
  // given
  const selection = changed(setModelChecked(emptySelection(), model("m1"), true, null));

  // when
  const next = changed(setModelChecked(selection, model("m1"), false, null));

  // then
  assert.deepEqual(next.picked, []);
  assert.deepEqual(next.leftOut, []);
  assert.equal(isEmptySelection(next), true);
});

test("should leave a model out when it is unchecked on a whole search and take it back when checked again", () => {
  // given
  const selection = changed(addWholeSearch(emptySelection(), ensembles));
  assert.equal(isModelChecked(selection, "m1", ensembles.query), true);

  // when
  const leftOut = changed(setModelChecked(selection, model("m1"), false, ensembles.query));
  const takenBack = changed(setModelChecked(leftOut, model("m1"), true, ensembles.query));

  // then
  assert.deepEqual(leftOut.leftOut, [model("m1")]);
  assert.equal(isModelChecked(leftOut, "m1", ensembles.query), false);
  assert.deepEqual(takenBack.leftOut, []);
  assert.deepEqual(takenBack.picked, [], "a model of the whole search needs no pick of its own");
  assert.equal(isModelChecked(takenBack, "m1", ensembles.query), true);
});

test("should show whole-search models as checked only on that search's results", () => {
  // given
  const selection = changed(addWholeSearch(emptySelection(), ensembles));

  // then
  assert.equal(isModelChecked(selection, "m1", ensembles.query), true);
  assert.equal(isModelChecked(selection, "m1", "model_type=Deposited"), false);
  assert.equal(isModelChecked(selection, "m1", null), false);
});

test("should keep a left-out model unchecked even when it is also picked elsewhere", () => {
  // given a model picked, then left out of a whole search
  let selection = changed(setModelChecked(emptySelection(), model("m1"), true, null));
  selection = changed(addWholeSearch(selection, ensembles));

  // when
  selection = changed(setModelChecked(selection, model("m1"), false, ensembles.query));

  // then exclude_model_ids wins over every other part, as the API applies it
  assert.deepEqual(selection.picked, []);
  assert.deepEqual(selection.leftOut, [model("m1")]);
  assert.equal(isModelChecked(selection, "m1", null), false);
});

test("should refuse picks, whole searches and left-out models past the API's limits", () => {
  // given
  const full = {
    ...emptySelection(),
    picked: Array.from({ length: MAX_PICKED_MODELS }, (_, index) => model(`p${index}`)),
    searches: Array.from({ length: MAX_WHOLE_SEARCHES }, (_, index) =>
      wholeSearchFromQuery(`organism=O${index}`, 1, true),
    ),
    leftOut: Array.from({ length: MAX_LEFT_OUT_MODELS }, (_, index) => model(`l${index}`)),
  };

  // then
  assert.deepEqual(setModelChecked(full, model("new"), true, null), { limit: "picked" });
  assert.deepEqual(addWholeSearch(full, ensembles), { limit: "searches" });
  assert.deepEqual(setModelChecked(full, model("new"), false, "organism=O1"), { limit: "left_out" });
  assert.equal(changed(addWholeSearch(full, wholeSearchFromQuery("organism=O1", 1, true))), full);
  for (const limit of ["picked", "searches", "left_out"]) {
    assert.match(limitMessage(limit), /at most/);
  }
  assert.throws(() => limitMessage("other"), /unknown selection limit: other/);
});

test("should build the POST /v1/selections body from the working selection", () => {
  // given
  let selection = changed(setModelChecked(emptySelection(), model("m1"), true, null));
  selection = changed(addWholeSearch(selection, ensembles));
  selection = changed(setModelChecked(selection, model("m2"), false, ensembles.query));
  selection = withAddDepositedModels(selection, true);

  // when
  const body = selectionRequest(selection);

  // then
  assert.deepEqual(body, {
    model_ids: ["m1"],
    searches: ["model_type=Ensemble"],
    exclude_model_ids: ["m2"],
    add_deposited_models: true,
  });
});

test("should reuse a saved selection only while the working selection is unchanged", () => {
  // given
  const first = changed(setModelChecked(emptySelection(), model("m1"), true, null));
  const both = changed(setModelChecked(first, model("m2"), true, null));
  const saved = withFrozen(both, {
    id: "sel-1",
    fingerprint: selectionFingerprint(both),
    modelCount: 2,
    entryCount: 1,
    createdAt: "2026-11-13T10:00:00Z",
  });
  const reordered = { ...saved, picked: [...saved.picked].reverse() };

  // then
  assert.equal(reusableSelectionId(saved), "sel-1");
  assert.equal(reusableSelectionId(reordered), "sel-1", "the order of picks does not change the selection");
  assert.equal(reusableSelectionId(removePicked(saved, "m2")), null);
  assert.equal(reusableSelectionId(withAddDepositedModels(saved, true)), null);
  assert.equal(reusableSelectionId(emptySelection()), null);
});

test("should say how many models the selection holds and when that is an estimate", () => {
  // given
  const picked = changed(setModelChecked(emptySelection(), model("m1"), true, null));
  const withSearch = changed(addWholeSearch(picked, ensembles));
  const leftOne = changed(setModelChecked(withSearch, model("m2"), false, ensembles.query));
  const capped = changed(addWholeSearch(emptySelection(), wholeSearchFromQuery("organism=X", 10000, false)));
  const uncounted = changed(addWholeSearch(emptySelection(), wholeSearchFromQuery("organism=Y", 0, false)));

  // then
  assert.equal(selectionSizeText(selectionSize(picked)), "1 model");
  assert.equal(selectionSizeText(selectionSize(leftOne)), "about 120 models");
  assert.equal(selectionSizeText(selectionSize(withAddDepositedModels(picked, true))), "about 1 model");
  assert.equal(selectionSizeText(selectionSize(capped)), "more than 10,000 models");
  assert.equal(selectionSizeText(selectionSize(uncounted)), "an uncounted number of models");
});

test("should give a capped selection as a sure lower bound, not the sum of overlapping searches", () => {
  // given two capped searches that may overlap, and one left-out model
  let selection = changed(addWholeSearch(emptySelection(), wholeSearchFromQuery("resolution_max=2", 10000, false)));
  selection = changed(addWholeSearch(selection, wholeSearchFromQuery("resolution_max=3", 10000, false)));
  const leftOne = changed(setModelChecked(selection, model("m1"), false, "resolution_max=2"));

  // then
  assert.equal(selectionSizeText(selectionSize(selection)), "more than 10,000 models");
  assert.equal(selectionSizeText(selectionSize(leftOne)), "more than 9,999 models");
});

test("should count left-out models only where they still apply once no whole search remains", () => {
  // given a whole search with model A left out, and model B picked
  let selection = changed(addWholeSearch(emptySelection(), ensembles));
  selection = changed(setModelChecked(selection, model("a"), false, ensembles.query));
  selection = changed(setModelChecked(selection, model("b"), true, null));

  // when the whole search is removed
  const withoutSearch = removeWholeSearch(selection, ensembles.query);

  // then B alone is selected, as the API would freeze it
  assert.equal(selectionSizeText(selectionSize(withoutSearch)), "1 model");
  assert.deepEqual(selectionRequest(withoutSearch).model_ids, ["b"]);
  assert.deepEqual(selectionRequest(withoutSearch).exclude_model_ids, ["a"]);
});

test("should name a whole search by its filters", () => {
  // then
  assert.equal(
    wholeSearchLabel(readModelSearch(new URLSearchParams("model_type=Ensemble&model_type=Deposited&r_free_max=0.2"))),
    "Model type: Ensemble, Deposited · R-free ≤ 0.2",
  );
  assert.equal(
    wholeSearchLabel(readModelSearch(new URLSearchParams("resolution_min=1&resolution_max=2"))),
    "Resolution 1–2 Å",
  );
  assert.equal(wholeSearchLabel(readModelSearch(new URLSearchParams(""))), "Every model");
  assert.equal(ensembles.label, "Model type: Ensemble");
});

test("should remove whole searches and take left-out models back", () => {
  // given
  let selection = changed(addWholeSearch(emptySelection(), ensembles));
  selection = changed(setModelChecked(selection, model("m2"), false, ensembles.query));

  // then
  assert.deepEqual(removeLeftOut(selection, "m2").leftOut, []);
  assert.deepEqual(removeWholeSearch(selection, ensembles.query).searches, []);
});

test("should keep the followed archive when the selection is cleared", () => {
  // given
  const download = { id: "d1", selectionId: "s1", fileChoice: "all_files", retryIds: ["r1"] };
  const selection = withDownload(changed(setModelChecked(emptySelection(), model("m1"), true, null)), download);

  // when
  const cleared = clearedSelection(selection);

  // then
  assert.equal(isEmptySelection(cleared), true);
  assert.deepEqual(cleared.download, download);
});

test("should read back what it stored and drop what it cannot read", () => {
  // given
  let selection = changed(setModelChecked(emptySelection(), model("m1"), true, null));
  selection = changed(addWholeSearch(selection, ensembles));
  selection = withDownload(selection, { id: "d1", selectionId: "s1", fileChoice: "original_mmcif", retryIds: ["r1"] });

  // then
  assert.deepEqual(parseStoredSelection(serializeSelection(selection)), { selection, problem: "none" });
  assert.deepEqual(parseStoredSelection(null), { selection: emptySelection(), problem: "none" });
  const beforeRetries = JSON.parse(serializeSelection(selection));
  delete beforeRetries.download.retryIds;
  assert.deepEqual(
    parseStoredSelection(JSON.stringify(beforeRetries)).selection.download,
    { id: "d1", selectionId: "s1", fileChoice: "original_mmcif", retryIds: [] },
    "an archive stored before retries were tracked has none",
  );
  const unreadable = [
    "not json",
    JSON.stringify({ ...JSON.parse(serializeSelection(selection)), version: 2 }),
    JSON.stringify({ ...JSON.parse(serializeSelection(selection)), picked: [{ id: "" }] }),
    JSON.stringify({ ...JSON.parse(serializeSelection(selection)), addDepositedModels: "yes" }),
    JSON.stringify({ ...JSON.parse(serializeSelection(selection)), download: { id: "d1", selectionId: "s1", fileChoice: "pdf", retryIds: [] } }),
    JSON.stringify({ ...JSON.parse(serializeSelection(selection)), download: { id: "d1", selectionId: "s1", fileChoice: "all_files", retryIds: [""] } }),
    JSON.stringify({ ...JSON.parse(serializeSelection(selection)), searches: [{ query: "a", label: "a", count: -1, countExact: true }] }),
    JSON.stringify([]),
  ];
  for (const raw of unreadable) {
    assert.deepEqual(parseStoredSelection(raw), { selection: emptySelection(), problem: "unreadable" }, raw);
  }
});

test("should offer exactly the API's file choices, each in words", () => {
  // then
  assert.deepEqual([...FILE_CHOICES].sort(), [...downloadFileChoiceValues].sort());
  for (const choice of FILE_CHOICES) {
    const text = fileChoiceText(choice);
    assert.ok(text.label.length > 0 && text.description.length > 0);
  }
  assert.match(fileChoiceText("original_mmcif").description, /Deposited models/);
  assert.throws(() => fileChoiceText("pdf"), /unknown file choice: pdf/);
});

function size(kind, extra = {}) {
  return { kind, measures: "bytes", ...extra };
}

/** A preview as patch 21's worker writes it. */
function preview(extra = {}) {
  return {
    records: { entries: 2, models: 3, models_without_matching_files: 1 },
    files: {
      total: 5,
      by_type: [
        { type: "model", files: 3, size: size("exact", { bytes: 3_000_000 }) },
        { type: "structure_factors", files: 2, size: size("exact", { bytes: 2_000_000_000 }) },
      ],
      availability: { included: 3, linked: 1, unavailable: 1, changed_at_source: 0 },
      not_included: [
        { code: "over_copy_limit", status: "linked", reason: "over 1 GiB", files: 1 },
        { code: "copy_missing", status: "unavailable", reason: "our copy is missing", files: 1 },
      ],
      verification: { verified: 3, mismatch: 0, not_verified: 2 },
    },
    sizes: {
      included: size("exact", { bytes: 1_003_000_000 }),
      archive: size("estimate", { bytes: 1_004_000_000, tolerance: "an upper bound" }),
      linked: size("exact", { bytes: 2_000_000_000, files_size_unknown: 1 }),
    },
    related_datasets: { total: 1, with_doi: 1, with_accession_only: 0, without_identifier: 0 },
    over_limit: false,
    ...extra,
  };
}

function attributes(status, extra = {}) {
  return {
    selection_id: "s1",
    file_choice: "all_files",
    status,
    prepare_requested: true,
    created_at: "2026-11-13T10:00:00Z",
    max_archive_bytes: 10_000_000_000,
    ...extra,
  };
}

test("should show every archive status the API sends", () => {
  // given
  const ready = {
    archive_url: "https://files.example/a.zip",
    manifest_url: "https://files.example/manifest.tsv",
    archive_bytes: 1_500_000_000,
    archive_sha256: "a".repeat(64),
    expires_at: "2026-11-20T10:00:00Z",
    counts: { included: 3, linked: 1, unavailable: 0, changed_at_source: 2 },
  };

  // then every status of the API has a view
  for (const status of downloadStatusValues) {
    assert.ok(downloadView(attributes(status, ready)).kind.length > 0, status);
  }
  for (const status of ["queued", "planning", "planned", "preparing"]) {
    assert.equal(downloadInProgress(downloadView(attributes(status))), true, status);
  }
  assert.equal(downloadView(attributes("planned")).step, "Waiting to be built");
  const waiting = downloadView(attributes("planned", { prepare_requested: false, preview: preview() }));
  assert.equal(waiting.kind, "preview", "planned without prepare waits for the reader, so polling stops");
  assert.equal(downloadInProgress(waiting), false);
  assert.equal(waiting.preview.headline, "3 models in 2 entries · 5 files");
  assert.throws(() => downloadView(attributes("planned", { prepare_requested: false })), /without its preview/);
  assert.deepEqual(downloadView(attributes("ready", { ...ready, retryable_files: 2, retry_of: "first" })), {
    kind: "ready",
    archiveUrl: ready.archive_url,
    manifestUrl: ready.manifest_url,
    archiveBytes: ready.archive_bytes,
    archiveSha256: ready.archive_sha256,
    expiresAt: ready.expires_at,
    counts: ready.counts,
    retryableFiles: 2,
    retryOf: "first",
  });
  assert.equal(downloadView(attributes("ready", ready)).retryableFiles, 0);
  assert.deepEqual(downloadView(attributes("blocked", { message: "over by 2 GB" })), {
    kind: "blocked",
    message: "over by 2 GB",
    preview: null,
    maxArchiveBytes: 10_000_000_000,
  });
  assert.deepEqual(downloadView(attributes("failed", { message: "worker gave up", retry_of: "first" })), {
    kind: "failed",
    message: "worker gave up",
    retryOf: "first",
  });
  assert.equal(downloadView(attributes("expired")).retryOf, null, "a first archive is asked for again as a new one");
  assert.equal(downloadView(attributes("expired", { retry_of: "first" })).retryOf, "first");
  const blockedWithPreview = downloadView(attributes("blocked", { preview: preview({ over_limit: true }) }));
  assert.equal(blockedWithPreview.preview.overLimit, true);
  assert.match(downloadView(attributes("blocked")).message, /more than 10 GB/);
  assert.equal(downloadView(attributes("failed")).kind, "failed");
  assert.equal(downloadView(attributes("expired")).kind, "expired");
  assert.equal(downloadInProgress(downloadView(attributes("ready", ready))), false);
});

test("should refuse an archive status it does not know, and a ready archive without its URL", () => {
  // then
  assert.throws(() => downloadView(attributes("archived")), /unknown download status: archived/);
  assert.throws(() => downloadView(attributes("ready")), /without archive_url/);
});

test("should list the archive's counts in the manifest's order", () => {
  // when
  const lines = countLines({ included: 3, linked: 1, unavailable: 0, changed_at_source: 2 });

  // then
  assert.deepEqual(
    lines.map((line) => [line.key, line.count]),
    [
      ["included", 3],
      ["linked", 1],
      ["unavailable", 0],
      ["changed_at_source", 2],
    ],
  );
});

test("should write sizes in decimal units, as the 10 GB limit is", () => {
  // then
  assert.equal(formatDecimalBytes(1), "1 byte");
  assert.equal(formatDecimalBytes(999), "999 bytes");
  assert.equal(formatDecimalBytes(1000), "1 kB");
  assert.equal(formatDecimalBytes(1_500_000_000), "1.5 GB");
  assert.equal(formatDecimalBytes(10_000_000_000), "10 GB");
  assert.equal(formatDecimalBytes(123_456_789), "123 MB");
});

test("should explain every problem and refuse one it does not know", () => {
  // given
  const codes = [
    "off",
    "empty",
    "too_large",
    "forbidden",
    "invalid",
    "not_found",
    "rate_limited",
    "selection_busy",
    "busy",
    "full",
    "timeout",
    "unreachable",
    "failed",
  ];

  // then
  for (const code of codes) {
    const message = problemMessage({ code, detail: null, retryAfterSeconds: 7 });
    assert.ok(message.title.length > 0 && message.body.length > 0, code);
  }
  assert.match(problemMessage({ code: "rate_limited", detail: null, retryAfterSeconds: 7 }).body, /in 7 seconds/);
  assert.equal(
    problemMessage({ code: "forbidden", detail: "flagged needs the reviewer role", retryAfterSeconds: null }).body,
    "flagged needs the reviewer role",
  );
  assert.throws(() => problemMessage({ code: "other", detail: null, retryAfterSeconds: null }), /unknown download problem/);
});

test("should state the reuse terms exactly as the researchers worded them", () => {
  // then
  assert.equal(
    REUSE_TERMS,
    "These data are dedicated to the public domain under CC0 1.0. We ask that you acknowledge Dynamic PDB and cite the depositors of the entries you use.",
  );
  assert.equal(REUSE_LICENSE_URL, "https://creativecommons.org/publicdomain/zero/1.0/");
});

test("should put the preview in words, every size as exact, estimate or unknown", () => {
  // when
  const summary = previewSummary(preview(), 10_000_000_000);

  // then
  assert.equal(summary.headline, "3 models in 2 entries · 5 files");
  assert.equal(summary.modelsWithoutFiles, 1);
  assert.deepEqual(
    summary.byType.map((item) => [item.label, item.files, item.size]),
    [
      ["Coordinate files", 3, "3 MB"],
      ["Structure factors", 2, "2 GB"],
    ],
  );
  assert.equal(summary.included, "1 GB");
  assert.equal(summary.archive, "about 1 GB (an upper bound)");
  assert.equal(summary.linked, "2 GB, plus 1 file of unknown size");
  assert.deepEqual(summary.leftOut.map((item) => [item.code, item.files]), [["over_copy_limit", 1], ["copy_missing", 1]]);
  assert.deepEqual(summary.verification, { verified: 3, mismatch: 0, notVerified: 2 });
  assert.match(summary.relatedDatasets, /1 related dataset/);
  assert.equal(summary.includedBytes, 1_003_000_000);
  assert.equal(summary.overLimit, false);
  assert.equal(sizeText(size("unknown")), "unknown: no size is recorded");
  for (const kind of downloadSizeKindValues) {
    assert.ok(sizeText(size(kind, { bytes: 1 })).length > 0, kind);
  }
  assert.throws(() => sizeText(size("guess")), /unknown size kind: guess/);
});

test("should say every reason, file type and availability the API can send, and refuse others", () => {
  // then
  for (const code of downloadReasonCodeValues) {
    assert.ok(reasonCodeText(code).length > 0, code);
  }
  assert.match(reasonCodeText("read_failed"), /a retry may fetch it/);
  for (const type of artifactTypeValues) {
    assert.ok(artifactTypeLabel(type).length > 0, type);
  }
  for (const status of downloadFileAvailabilityValues) {
    assert.ok(availabilityText(status).length > 0, status);
  }
  assert.throws(() => reasonCodeText("lost"), /unknown reason code: lost/);
  assert.throws(() => artifactTypeLabel("video"), /unknown file type: video/);
  assert.throws(() => availabilityText("gone"), /unknown file availability: gone/);
});

test("should add each retry once, under the archive being followed", () => {
  // given
  const selection = withDownload(emptySelection(), { id: "d1", selectionId: "s1", fileChoice: "all_files", retryIds: [] });

  // when
  const once = withRetry(selection, "r1");
  const twice = withRetry(once, "r1");
  const second = withRetry(twice, "r2");

  // then
  assert.deepEqual(second.download.retryIds, ["r1", "r2"]);
  assert.equal(withRetry(selection, "d1"), selection, "the first archive is not its own retry");
  assert.equal(withRetry(emptySelection(), "r1").download, null);
});

test("should explain the new archive problems", () => {
  // then
  for (const code of ["not_preparable", "nothing_to_retry", "not_retryable"]) {
    const message = problemMessage({ code, detail: null, retryAfterSeconds: null });
    assert.ok(message.title.length > 0 && message.body.length > 0, code);
  }
});
