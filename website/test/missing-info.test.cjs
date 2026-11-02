require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

const { missingInfoLines, missingInfoLinesForPage } = require("../src/lib/missing-info.ts");
const {
  default: MissingInfoNotice,
  MISSING_INFO_TOOLTIP,
} = require("../src/app/components/MissingInfoNotice.tsx");

function metric(id, key) {
  return { id, key, value: 0.2, created_at: "2026-08-13T00:00:00Z" };
}

const CONTEXT = {
  metrics: [metric("m-free", "r_free"), metric("m-work", "r_work"), metric("m-wilson", "wilson_b")],
  runs: new Map([
    ["run-refmac", { name: "REFMAC", software: null }],
    ["run-refine", { name: "refinement", software: "PHENIX" }],
  ]),
};

test("should_word_every_gap_in_reading_order_when_a_model_lacks_information", () => {
  // given
  const gaps = [
    { kind: "model_run" },
    { kind: "run_software_version", run_id: "run-refmac" },
    { kind: "metric_software", metric_id: "m-work" },
    { kind: "metric_software", metric_id: "m-free" },
    { kind: "run_software_version", run_id: "run-refine" },
  ];

  // when
  const lines = missingInfoLines(gaps, CONTEXT);

  // then
  assert.deepEqual(lines, [
    "Software not recorded for metrics: R-work, R-free",
    'Software version not recorded for run "REFMAC"',
    'Software version not recorded for run "refinement" (PHENIX)',
    "No run recorded: how this computed model was made is not on file",
  ]);
});

test("should_name_one_metric_by_its_key_when_it_has_no_display_label", () => {
  // when
  const lines = missingInfoLines([{ kind: "metric_software", metric_id: "m-wilson" }], CONTEXT);

  // then
  assert.deepEqual(lines, ["Software not recorded for metric: wilson_b"]);
});

test("should_return_no_lines_when_nothing_is_missing_or_the_backend_did_not_say", () => {
  // when
  const empty = missingInfoLines([], CONTEXT);
  const unknown = missingInfoLines(undefined, CONTEXT);

  // then
  assert.deepEqual(empty, []);
  assert.deepEqual(unknown, []);
});

test("should_throw_when_a_gap_cannot_be_worded", () => {
  // given
  const cases = {
    "unknown kind": [{ kind: "metric_unit" }],
    "unknown metric": [{ kind: "metric_software", metric_id: "m-gone" }],
    "unknown run": [{ kind: "run_software_version", run_id: "run-gone" }],
  };

  for (const [name, gaps] of Object.entries(cases)) {
    // when / then
    assert.throws(() => missingInfoLines(gaps, CONTEXT), Error, name);
  }
});

test("should_render_without_the_label_when_a_gap_cannot_be_worded_on_the_page", (t) => {
  // given
  const logged = t.mock.method(console, "error", () => {});

  // when
  const lines = missingInfoLinesForPage([{ kind: "metric_unit" }], CONTEXT);

  // then
  assert.deepEqual(lines, []);
  assert.equal(logged.mock.callCount(), 1);
});

test("should_show_the_label_with_its_lines_when_the_model_lacks_information", () => {
  // given
  const lines = ['Software version not recorded for run "REFMAC"'];

  // when
  const html = renderToStaticMarkup(React.createElement(MissingInfoNotice, { lines }));

  // then
  assert.ok(html.includes("Information missing"));
  assert.ok(html.includes(`title="${MISSING_INFO_TOOLTIP}"`));
  assert.ok(html.includes("Software version not recorded for run &quot;REFMAC&quot;"));
  assert.ok(html.includes("The record stays public."));
});

test("should_show_nothing_when_the_model_lacks_nothing", () => {
  // when
  const html = renderToStaticMarkup(React.createElement(MissingInfoNotice, { lines: [] }));

  // then
  assert.equal(html, "");
});
