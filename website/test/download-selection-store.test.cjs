require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  createSelectionStore,
  UNREADABLE_SELECTION_NOTICE,
  UNSTORED_SELECTION_NOTICE,
} = require("../src/lib/download-selection-store.ts");
const {
  clearedSelection,
  emptySelection,
  MAX_WHOLE_SEARCHES,
  serializeSelection,
  setModelChecked,
  addWholeSearch,
  wholeSearchFromQuery,
} = require("../src/lib/download-selection.ts");

function model(id) {
  return { id, title: `Model ${id}`, entryId: "dpdb_entry1", entryLabel: "dpdb_entry1" };
}

/** A store over an in-memory storage, with starts answered by hand. */
function harness({ stored = null, writable = true } = {}) {
  let value = stored;
  const starts = [];
  const actions = [];
  const renewals = [];
  const action = (kind) => (downloadId) =>
    new Promise((resolve, reject) => {
      actions.push({ kind, downloadId, resolve, reject });
    });
  const store = createSelectionStore({
    storage: {
      read: () => value,
      write: (next) => {
        if (!writable) return false;
        value = next;
        return true;
      },
    },
    start: (input) =>
      new Promise((resolve, reject) => {
        starts.push({ input, resolve, reject });
      }),
    prepare: action("prepare"),
    retry: action("retry"),
    renew: (selectionId, fileChoice) =>
      new Promise((resolve, reject) => {
        renewals.push({ selectionId, fileChoice, resolve, reject });
      }),
  });
  return { store, starts, actions, renewals, stored: () => value };
}

const saved = { id: "sel-1", modelCount: 1, entryCount: 1, createdAt: "2026-11-13T10:00:00Z" };

function started(downloadId = "dl-1", selectionId = "sel-1") {
  return {
    status: "started",
    saved,
    download: {
      id: downloadId,
      attributes: {
        selection_id: selectionId,
        file_choice: "all_files",
        status: "queued",
        prepare_requested: true,
        created_at: "2026-11-13T10:00:00Z",
        max_archive_bytes: 10_000_000_000,
      },
    },
  };
}

const pick = (id) => (selection) => setModelChecked(selection, model(id), true, null);

test("should read the stored selection once and say when it could not", () => {
  // given
  const readable = harness({ stored: serializeSelection({ ...emptySelection(), picked: [model("m1")] }) });
  const broken = harness({ stored: "{not json" });

  // when
  readable.store.load();
  readable.store.load();
  broken.store.load();

  // then
  assert.equal(readable.store.getSnapshot().loaded, true);
  assert.deepEqual(readable.store.getSnapshot().selection.picked, [model("m1")]);
  assert.equal(broken.store.getSnapshot().notice, UNREADABLE_SELECTION_NOTICE);
});

test("should keep changes in storage, and say so when the browser refuses", () => {
  // given
  const kept = harness();
  const refused = harness({ writable: false });

  // when
  kept.store.apply(pick("m1"));
  refused.store.apply(pick("m1"));

  // then
  assert.deepEqual(JSON.parse(kept.stored()).picked, [model("m1")]);
  assert.equal(kept.store.getSnapshot().notice, null);
  assert.deepEqual(refused.store.getSnapshot().selection.picked, [model("m1")], "the tab still has it");
  assert.equal(refused.store.getSnapshot().notice, UNSTORED_SELECTION_NOTICE);
});

test("should leave a notice instead of a change past the API's limits", () => {
  // given
  const run = harness();
  for (let index = 0; index < MAX_WHOLE_SEARCHES; index += 1) {
    run.store.apply((selection) => addWholeSearch(selection, wholeSearchFromQuery(`organism=O${index}`, 1, true)));
  }

  // when
  run.store.apply((selection) => addWholeSearch(selection, wholeSearchFromQuery("organism=X", 1, true)));

  // then
  assert.equal(run.store.getSnapshot().selection.searches.length, MAX_WHOLE_SEARCHES);
  assert.match(run.store.getSnapshot().notice, /at most 10 whole searches/);
});

test("should land a finished start in the selection as it is now, keeping changes made meanwhile", async () => {
  // given a start in flight
  const run = harness();
  run.store.load();
  run.store.apply(pick("m1"));
  const starting = run.store.startDownload("all_files");
  assert.equal(run.store.getSnapshot().pending, "start");

  // when the reader picks another model, as on another page, before the start finishes
  run.store.apply(pick("m2"));
  run.starts[0].resolve(started());
  await starting;

  // then both picks stay, and the archive is followed
  const state = run.store.getSnapshot();
  assert.deepEqual(state.selection.picked.map((item) => item.id), ["m1", "m2"]);
  assert.deepEqual(state.selection.download, { id: "dl-1", selectionId: "sel-1", fileChoice: "all_files", retryIds: [] });
  assert.equal(state.pending, null);
  assert.deepEqual(run.starts[0].input.selection.model_ids, ["m1"], "the start sent the selection as it was");
  assert.deepEqual(JSON.parse(run.stored()).download, state.selection.download);
});

test("should ignore a second start while one is in flight", async () => {
  // given
  const run = harness();
  run.store.apply(pick("m1"));
  const first = run.store.startDownload("all_files");

  // when
  await run.store.startDownload("original_mmcif");

  // then
  assert.equal(run.starts.length, 1);
  run.starts[0].resolve(started());
  await first;
});

test("should keep the saved selection when asking for its archive failed, and reuse it next time", async () => {
  // given
  const run = harness();
  run.store.apply(pick("m1"));
  const first = run.store.startDownload("all_files");
  run.starts[0].resolve({
    status: "failed",
    saved,
    problem: { code: "busy", detail: null, retryAfterSeconds: 30 },
  });
  await first;
  assert.deepEqual(run.store.getSnapshot().problem.operation, "start");
  assert.equal(run.store.getSnapshot().problem.problem.code, "busy");

  // when
  const second = run.store.startDownload("all_files");

  // then
  assert.equal(run.starts[1].input.savedSelectionId, "sel-1");
  assert.equal(run.store.getSnapshot().problem, null);
  run.starts[1].resolve(started());
  await second;
});

test("should follow the archive afresh after every successful start, even under the same ID", async () => {
  // given
  const run = harness();
  run.store.apply(pick("m1"));
  const first = run.store.startDownload("all_files");
  run.starts[0].resolve(started("dl-1"));
  await first;
  const round = run.store.getSnapshot().startRound;

  // when the server returns the same archive again
  const second = run.store.startDownload("all_files");
  run.starts[1].resolve(started("dl-1"));
  await second;

  // then
  assert.equal(run.store.getSnapshot().startRound, round + 1);
  assert.equal(run.store.getSnapshot().selection.download.id, "dl-1");
});

test("should report an unreachable server when the start itself fails", async () => {
  // given
  const run = harness();
  run.store.apply(pick("m1"));
  const starting = run.store.startDownload("all_files");

  // when
  run.starts[0].reject(new Error("network down"));
  await starting;

  // then
  assert.equal(run.store.getSnapshot().pending, null);
  assert.equal(run.store.getSnapshot().problem.problem.code, "unreachable");
});

test("should tell subscribers about every change until they leave", () => {
  // given
  const run = harness();
  let calls = 0;
  const leave = run.store.subscribe(() => {
    calls += 1;
  });

  // when
  run.store.apply(pick("m1"));
  run.store.dismissNotice();
  leave();
  run.store.apply(pick("m2"));

  // then
  assert.equal(calls, 2);
});

/** A store following archive dl-1 of selection sel-1. */
async function following() {
  const run = harness();
  run.store.apply(pick("m1"));
  const starting = run.store.startDownload("all_files");
  run.starts[0].resolve(started("dl-1"));
  await starting;
  return run;
}

function done(downloadId) {
  return { status: "done", download: started(downloadId).download };
}

test("should build the followed archive and follow it afresh", async () => {
  // given
  const run = await following();
  const round = run.store.getSnapshot().startRound;

  // when
  const preparing = run.store.prepareDownload("dl-1");
  assert.equal(run.store.getSnapshot().pending, "prepare");
  run.actions[0].resolve(done("dl-1"));
  await preparing;

  // then
  assert.deepEqual(run.actions.map((item) => [item.kind, item.downloadId]), [["prepare", "dl-1"]]);
  assert.equal(run.store.getSnapshot().pending, null);
  assert.equal(run.store.getSnapshot().startRound, round + 1);
});

test("should list a retry under the archive it retries, once, and keep it across a new start of the same archive", async () => {
  // given
  const run = await following();

  // when
  const retrying = run.store.retryFailedFiles("dl-1");
  run.actions[0].resolve(done("retry-1"));
  await retrying;
  const again = run.store.retryFailedFiles("dl-1");
  run.actions[1].resolve(done("retry-1"));
  await again;
  const restart = run.store.startDownload("all_files");
  run.starts[1].resolve(started("dl-1"));
  await restart;

  // then
  assert.deepEqual(run.store.getSnapshot().selection.download.retryIds, ["retry-1"]);
  assert.deepEqual(JSON.parse(run.stored()).download.retryIds, ["retry-1"]);
});

test("should drop the retries when a start follows a different archive", async () => {
  // given
  const run = await following();
  const retrying = run.store.retryFailedFiles("dl-1");
  run.actions[0].resolve(done("retry-1"));
  await retrying;

  // when
  const restart = run.store.startDownload("original_mmcif");
  run.starts[1].resolve(started("dl-2"));
  await restart;

  // then
  assert.equal(run.store.getSnapshot().selection.download.id, "dl-2");
  assert.deepEqual(run.store.getSnapshot().selection.download.retryIds, []);
});

test("should report a failed prepare or retry under its archive, and run one operation at a time", async () => {
  // given
  const run = await following();

  // when one retry waits, another operation does nothing
  const retrying = run.store.retryFailedFiles("dl-1");
  await run.store.prepareDownload("dl-1");
  await run.store.startDownload("all_files");
  assert.equal(run.actions.length, 1);
  assert.equal(run.starts.length, 1);
  run.actions[0].resolve({ status: "failed", problem: { code: "nothing_to_retry", detail: null, retryAfterSeconds: null } });
  await retrying;

  // then
  assert.deepEqual(run.store.getSnapshot().problem, {
    operation: "retry",
    archiveId: "dl-1",
    problem: { code: "nothing_to_retry", detail: null, retryAfterSeconds: null },
  });
  assert.deepEqual(run.store.getSnapshot().selection.download.retryIds, []);
});

test("should not act on an archive the selection does not follow", async () => {
  // given
  const run = await following();

  // when
  await run.store.prepareDownload("someone-elses");
  await run.store.retryFailedFiles("someone-elses");

  // then
  assert.equal(run.actions.length, 0);
});

test("should not file a retry under another archive that the selection moved to meanwhile", async () => {
  // given a retry of dl-1 in flight
  const run = await following();
  const retrying = run.store.retryFailedFiles("dl-1");

  // when another tab follows another archive before the retry answers
  run.store.update((selection) => ({
    ...selection,
    download: { id: "dl-2", selectionId: "sel-1", fileChoice: "original_mmcif", retryIds: [] },
  }));
  run.actions[0].resolve(done("retry-1"));
  await retrying;

  // then the other archive gets no retry of dl-1
  assert.equal(run.store.getSnapshot().selection.download.id, "dl-2");
  assert.deepEqual(run.store.getSnapshot().selection.download.retryIds, []);
  assert.equal(run.store.getSnapshot().pending, null);
});

test("should ask again for an expired archive with its own saved selection and files after the selection was cleared", async () => {
  // given an archive of sel-1 with a retry, then the working selection cleared
  const run = await following();
  const retrying = run.store.retryFailedFiles("dl-1");
  run.actions[0].resolve(done("retry-1"));
  await retrying;
  run.store.update(clearedSelection);
  const round = run.store.getSnapshot().startRound;

  // when
  const renewing = run.store.renewDownload("dl-1");
  assert.equal(run.store.getSnapshot().pending, "renew");
  run.renewals[0].resolve(done("dl-2"));
  await renewing;

  // then no selection is saved or sent: the archive's own one is asked for again
  assert.equal(run.starts.length, 1);
  assert.deepEqual(run.renewals.map((item) => [item.selectionId, item.fileChoice]), [["sel-1", "all_files"]]);
  assert.deepEqual(run.store.getSnapshot().selection.download, {
    id: "dl-2",
    selectionId: "sel-1",
    fileChoice: "all_files",
    retryIds: [],
  });
  assert.deepEqual(run.store.getSnapshot().selection.picked, []);
  assert.equal(run.store.getSnapshot().pending, null);
  assert.equal(run.store.getSnapshot().startRound, round + 1);
});

test("should ask again for an expired archive's own selection when the working selection changed", async () => {
  // given
  const run = await following();
  run.store.apply(pick("m2"));

  // when
  const renewing = run.store.renewDownload("dl-1");
  run.renewals[0].resolve(done("dl-1"));
  await renewing;

  // then the archive keeps its selection, and the working selection its new model
  assert.deepEqual(run.renewals.map((item) => item.selectionId), ["sel-1"]);
  assert.equal(run.store.getSnapshot().selection.download.selectionId, "sel-1");
  assert.deepEqual(
    run.store.getSnapshot().selection.picked.map((item) => item.id),
    ["m1", "m2"],
  );
});

test("should report a failed renewal under its archive, and renew only the first archive the selection follows", async () => {
  // given
  const run = await following();
  const retrying = run.store.retryFailedFiles("dl-1");
  run.actions[0].resolve(done("retry-1"));
  await retrying;

  // when a retry or an archive not followed is renewed, nothing is asked
  await run.store.renewDownload("retry-1");
  await run.store.renewDownload("someone-elses");
  assert.equal(run.renewals.length, 0);
  const renewing = run.store.renewDownload("dl-1");
  run.renewals[0].resolve({ status: "failed", problem: { code: "not_found", detail: null, retryAfterSeconds: null } });
  await renewing;

  // then
  assert.deepEqual(run.store.getSnapshot().problem, {
    operation: "renew",
    archiveId: "dl-1",
    problem: { code: "not_found", detail: null, retryAfterSeconds: null },
  });
  assert.deepEqual(run.store.getSnapshot().selection.download, {
    id: "dl-1",
    selectionId: "sel-1",
    fileChoice: "all_files",
    retryIds: ["retry-1"],
  });
});

test("should not move the selection back to an archive it renewed after another tab moved it on", async () => {
  // given a renewal of dl-1 in flight
  const run = await following();
  const renewing = run.store.renewDownload("dl-1");

  // when another tab follows another archive before the renewal answers
  run.store.update((selection) => ({
    ...selection,
    download: { id: "dl-3", selectionId: "sel-2", fileChoice: "original_mmcif", retryIds: [] },
  }));
  run.renewals[0].resolve(done("dl-2"));
  await renewing;

  // then
  assert.equal(run.store.getSnapshot().selection.download.id, "dl-3");
  assert.equal(run.store.getSnapshot().pending, null);
});
