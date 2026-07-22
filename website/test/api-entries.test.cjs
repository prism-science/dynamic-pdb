require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ApiRequestError,
  createEntry,
  getExperimentPageData,
  listEntries,
} = require("../src/lib/api/entries.ts");

test("should list entries with trimmed query and optional authorization", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    let requestedHeaders = {};
    global.fetch = async (url, init) => {
      requestedURL = String(url);
      requestedHeaders = init.headers;
      return jsonResponse({ items: [{ id: "entry-1", name: "Entry 1" }] });
    };

    const entries = await listEntries("token-123", { query: "  hemoglobin  " });

    assert.equal(requestedURL, "https://backend.example/v1/entries?query=hemoglobin");
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(entries, [{ id: "entry-1", name: "Entry 1" }]);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should throw ApiRequestError with status when create entry fails", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    global.fetch = async () => new Response(JSON.stringify({ code: "BAD_REQUEST" }), { status: 400 });

    await assert.rejects(
      () => createEntry("token-123", { name: "Entry" }),
      (error) =>
        error instanceof ApiRequestError &&
        error.status === 400 &&
        error.message === "Backend responded with 400",
    );
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should scope experiment page data to experiment entities and L0 baseline entities", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const entry = {
      id: "entry-1",
      name: "Entry",
      description: null,
      thumbnail_image_url: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const experiment = {
      id: "experiment-1",
      entry_id: "entry-1",
      name: "Experiment",
      description: null,
      thumbnail_image_url: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const baseline = entity("baseline", null, "data", "L0");
    const model = entity("model", "experiment-1", "model", "L2");
    const otherModel = entity("other-model", "experiment-2", "model", "L2");
    const relations = [
      relation("r1", "baseline", "model", "input_to"),
      relation("r2", "other-model", "baseline", "input_to"),
      relation("r3", "other-model", "missing", "output_of"),
    ];
    global.fetch = async (url) => {
      const path = new URL(String(url)).pathname;
      if (path === "/v1/entries/entry-1") {
        return jsonResponse(entry);
      }
      if (path === "/v1/entries/entry-1/experiments/experiment-1") {
        return jsonResponse(experiment);
      }
      if (path === "/v1/entries/entry-1/entities") {
        return jsonResponse({ items: [baseline, model, otherModel], relations });
      }
      throw new Error(`unexpected fetch ${url}`);
    };

    const data = await getExperimentPageData("token-123", "entry-1", "experiment-1");

    assert.deepEqual(
      data.entities.map((item) => item.id),
      ["baseline", "model"],
    );
    assert.deepEqual(
      data.relations.map((item) => item.id),
      ["r1", "r2"],
    );
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

function entity(id, experimentId, type, level) {
  return {
    id,
    entry_id: "entry-1",
    experiment_id: experimentId,
    type,
    level,
    name: id,
    payload: type === "metrics" ? {} : { file_url: `s3://dynamic-pdb/${id}` },
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function relation(id, source, target, relationType) {
  return {
    id,
    source_entity_id: source,
    target_entity_id: target,
    relation_type: relationType,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function jsonResponse(body, init = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  });
}

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}
