require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { flushPending, track } = require("../src/lib/analytics.ts");

test("should queue events raised before the script loads and send them once it does", () => {
  // given
  const sent = [];
  globalThis.window = {};

  // when
  track("search", { query: "lysozyme", results: 3 });
  window.umami = { track: (name, data) => sent.push([name, data]) };
  flushPending();
  track("viewer-open", { viewer: "hetstar" });

  // then
  assert.deepEqual(sent, [
    ["search", { query: "lysozyme", results: 3 }],
    ["viewer-open", { viewer: "hetstar" }],
  ]);
  delete globalThis.window;
});

test("should not break the page when the tracker throws", () => {
  // given
  globalThis.window = {
    umami: {
      track: () => {
        throw new Error("blocked");
      },
    },
  };

  // when / then
  assert.doesNotThrow(() => track("download-click", { file: "x.cif" }));
  delete globalThis.window;
});
