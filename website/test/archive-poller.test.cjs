require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { createArchivePoller, FIRST_POLL_MS, LONGEST_POLL_MS } = require("../src/lib/archive-poller.ts");

/** A poller with hand-driven timers, visibility and answers. */
function harness(outcomes) {
  const timers = [];
  const outcomesSeen = [];
  const pending = [];
  let hidden = false;
  let reads = 0;
  const poller = createArchivePoller({
    read: (signal) =>
      new Promise((resolve) => {
        reads += 1;
        pending.push({ resolve, signal });
      }),
    keepPolling: (outcome) => outcome === "working",
    onOutcome: (outcome) => outcomesSeen.push(outcome),
    isHidden: () => hidden,
    setTimer: (callback, ms) => {
      const timer = { callback, ms, cleared: false };
      timers.push(timer);
      return timer;
    },
    clearTimer: (timer) => {
      timer.cleared = true;
    },
  });
  return {
    poller,
    timers,
    outcomesSeen,
    get reads() {
      return reads;
    },
    get pendingCount() {
      return pending.length;
    },
    setHidden: (value) => {
      hidden = value;
    },
    async answer(outcome) {
      const next = pending.shift();
      next.resolve(outcome ?? outcomes.shift());
      await new Promise((resolve) => setImmediate(resolve));
    },
    async fireTimer() {
      const timer = timers.find((item) => !item.fired && !item.cleared);
      timer.fired = true;
      timer.callback();
      await new Promise((resolve) => setImmediate(resolve));
    },
  };
}

test("should keep one request in flight when the page is shown again while one waits", async () => {
  // given
  const run = harness([]);
  run.poller.start();
  assert.equal(run.reads, 1);

  // when the page is hidden and shown again before the answer
  run.setHidden(true);
  run.poller.visibilityChanged();
  run.setHidden(false);
  run.poller.visibilityChanged();

  // then no second request starts
  assert.equal(run.reads, 1);
  await run.answer("ready");
  assert.deepEqual(run.outcomesSeen, ["ready"]);
});

test("should stop asking after an answer that ends the wait, even when the page is shown again", async () => {
  // given
  const run = harness([]);
  run.poller.start();

  // when
  await run.answer("ready");
  run.poller.visibilityChanged();

  // then
  assert.equal(run.reads, 1);
  assert.equal(run.timers.length, 0);
});

test("should ask again with growing waits while the archive is being built", async () => {
  // given
  const run = harness([]);
  run.poller.start();

  // when
  for (let round = 0; round < 8; round += 1) {
    await run.answer("working");
    await run.fireTimer();
  }

  // then
  assert.equal(run.reads, 9);
  assert.equal(run.timers[0].ms, FIRST_POLL_MS);
  assert.equal(run.timers[1].ms, 3_000);
  assert.equal(run.timers.at(-1).ms, LONGEST_POLL_MS);
});

test("should wait while the page is hidden and ask once when it is shown", async () => {
  // given a poll due while the page is hidden
  const run = harness([]);
  run.poller.start();
  await run.answer("working");
  run.setHidden(true);
  await run.fireTimer();
  assert.equal(run.reads, 1);

  // when the page is shown, twice
  run.setHidden(false);
  run.poller.visibilityChanged();
  run.poller.visibilityChanged();

  // then
  assert.equal(run.reads, 2);
  assert.equal(run.pendingCount, 1);
});

test("should drop a late answer and its timer once stopped", async () => {
  // given
  const run = harness([]);
  run.poller.start();
  await run.answer("working");

  // when
  run.poller.stop();

  // then
  assert.equal(run.timers[0].cleared, true);
  run.poller.visibilityChanged();
  assert.equal(run.reads, 1);
});
