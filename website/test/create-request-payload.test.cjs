require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const entries = require("../src/lib/api/entries.ts");
const goldenRequests = require("./fixtures/create-request-golden.json");
const { captureRequests } = require("./fixtures/create-request-inputs.cjs");

// The golden file was captured from the hand-written request types that
// preceded the generated ones, so it pins the wire format across that change.
test("should_build_same_request_when_types_are_generated", async () => {
  // given
  const expectedRequests = goldenRequests;

  // when
  const requests = await captureRequests(entries);

  // then
  assert.deepEqual(Object.keys(requests), Object.keys(expectedRequests));
  for (const [name, expected] of Object.entries(expectedRequests)) {
    assert.equal(requests[name].url, expected.url, name);
    assert.equal(requests[name].method, expected.method, name);
    assert.equal(requests[name].body, expected.body, name);
  }
});

test("should_reject_metric_key_when_key_is_not_declared_by_the_api", async () => {
  // given
  const previousFetch = global.fetch;
  let fetchCalled = false;
  global.fetch = async () => {
    fetchCalled = true;
    throw new Error("fetch must not be reached");
  };
  const input = {
    title: "Model",
    entities: [
      {
        id: "00000000-0000-4000-8000-000000000005",
        type: "metrics",
        name: "Metrics",
        payload: { r_work: 0.2, not_a_metric: 1, constructor: 2 },
      },
    ],
  };

  try {
    // when
    const rejection = entries.createModel("token", "entry-1", input);

    // then
    await assert.rejects(rejection, /unknown metric key: not_a_metric/);
    assert.equal(fetchCalled, false);
  } finally {
    global.fetch = previousFetch;
  }
});

test("should_skip_non_numeric_values_when_key_is_not_a_metric", async () => {
  // given
  const previousFetch = global.fetch;
  let sentBody = "";
  global.fetch = async (_url, init) => {
    sentBody = init.body;
    return new Response(
      JSON.stringify({
        data: {
          type: "r",
          id: "1",
          attributes: { entry_id: "e", model_id: "m", revision_id: "r", state: "in_review" },
        },
      }),
      { status: 201, headers: { "Content-Type": "application/vnd.api+json" } },
    );
  };
  const input = {
    title: "Model",
    entities: [
      {
        id: "00000000-0000-4000-8000-000000000005",
        type: "metrics",
        name: "Metrics",
        payload: { r_work: 0.2, label: "text", rmsf: Number.NaN },
      },
    ],
  };

  try {
    // when
    await entries.createModel("token", "entry-1", input);

    // then
    const metrics = JSON.parse(sentBody).model.metrics;
    assert.deepEqual(metrics.map((metric) => metric.key), ["r_work"]);
  } finally {
    global.fetch = previousFetch;
  }
});
