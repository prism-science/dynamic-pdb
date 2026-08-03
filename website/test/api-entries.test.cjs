require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ApiRequestError,
  createEntry,
  getModelPageData,
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

test("should post create entry using the new backend graph shape", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    let request = null;
    global.fetch = async (url, init) => {
      request = { url: String(url), init, body: JSON.parse(init.body) };
      return new Response(null, { status: 201 });
    };

    await createEntry("token-123", {
      id: "entry-1",
      name: "Entry",
      entities: [
        {
          id: "baseline",
          type: "data",
          level: "L0",
          name: "sequence.fasta",
          payload: {
            file_url: "s3://dynamic-pdb/sequence.fasta",
            type: "fasta",
            size: 12,
            metadata: { records: [{ header: "A", sequence: "AC" }] },
          },
        },
      ],
      models: [
        {
          id: "model-1",
          name: "Model",
          entities: [
            {
              id: "model-artifact",
              type: "model",
              level: "L2",
              name: "model.cif",
              payload: {
                file_url: "s3://dynamic-pdb/model.cif",
                authors: ["Alice"],
                affiliation: "Lab",
              },
            },
            {
              id: "metrics-1",
              type: "metrics",
              level: "L3",
              name: "Metrics",
              payload: { r_free: 0.23 },
            },
            {
              id: "program-1",
              type: "program",
              level: null,
              name: "phenix.refine",
              payload: {
                name: "phenix.refine",
                version: "1.21.2",
                description: "Refinement",
              },
            },
          ],
          relations: [
            { source_entity_id: "baseline", target_entity_id: "program-1", relation_type: "input_to" },
            { source_entity_id: "model-artifact", target_entity_id: "program-1", relation_type: "output_of" },
          ],
        },
      ],
    });

    assert.equal(request.url, "https://backend.example/v1/entries");
    assert.equal(request.init.headers.Authorization, "Bearer token-123");
    assert.equal(request.body.entities, undefined);
    assert.deepEqual(request.body.artifacts, [
      {
        id: "baseline",
        name: "sequence.fasta",
        level: "L0",
        uri: "s3://dynamic-pdb/sequence.fasta",
        sha256: null,
        format: "fasta",
        size_bytes: 12,
        metadata: { records: [{ header: "A", sequence: "AC" }] },
      },
    ]);
    assert.equal(request.body.models[0].primary_artifact_id, "model-artifact");
    assert.deepEqual(request.body.models[0].metadata, {
      authors: ["Alice"],
      affiliation: "Lab",
    });
    assert.deepEqual(request.body.models[0].runs[0].artifacts, [
      { artifact_id: "baseline", direction: "input", position: null },
      { artifact_id: "model-artifact", direction: "output", position: null },
    ]);
    assert.match(request.body.models[0].metrics[0].id, /^[0-9a-f-]{36}$/);
    assert.deepEqual(
      {
        key: request.body.models[0].metrics[0].key,
        value: request.body.models[0].metrics[0].value,
      },
      { key: "r_free", value: 0.23 },
    );
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should build model page entities from model artifacts and runs", async () => {
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
    const model = {
      id: "model-1",
      entry_id: "entry-1",
      name: "Model",
      description: null,
      thumbnail_image_url: null,
      primary_artifact_id: "model-artifact",
      metrics: [{ id: "metric-1", key: "r_free", value: 0.23, created_at: "2026-01-01T00:00:00Z" }],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    global.fetch = async (url) => {
      const path = new URL(String(url)).pathname;
      if (path === "/v1/entries/entry-1") {
        return jsonResponse(entry);
      }
      if (path === "/v1/entries/entry-1/models/model-1") {
        return jsonResponse(model);
      }
      if (path === "/v1/entries/entry-1/models/model-1/artifacts") {
        return jsonResponse({
          items: [artifact("model-artifact", "model.cif", "L2", "mmcif")],
          runs: [run("program-1")],
          relations: [
            { run_id: "program-1", artifact_id: "baseline", direction: "input", position: null },
            { run_id: "program-1", artifact_id: "model-artifact", direction: "output", position: null },
          ],
        });
      }
      throw new Error(`unexpected fetch ${url}`);
    };

    const data = await getModelPageData("token-123", "entry-1", "model-1");

    assert.deepEqual(
      data.entities.map((item) => item.id),
      ["model-artifact", "program-1", "2048fe82-a812-54f7-b19d-11e15a6d97f2"],
    );
    assert.deepEqual(
      data.relations.map((item) => [
        item.source_entity_id,
        item.target_entity_id,
        item.relation_type,
      ]),
      [
        ["baseline", "program-1", "input_to"],
        ["model-artifact", "program-1", "output_of"],
        ["2048fe82-a812-54f7-b19d-11e15a6d97f2", "model-artifact", "metrics_for"],
      ],
    );
    assert.equal(data.entities[0].type, "model");
    assert.deepEqual(data.entities[2].payload, { r_free: 0.23 });
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

function artifact(id, name, level, format) {
  return {
    id,
    name,
    level,
    uri: `s3://dynamic-pdb/${id}`,
    sha256: null,
    format,
    size_bytes: 123,
    metadata: {},
    created_by: "user-1",
    created_at: "2026-01-01T00:00:00Z",
  };
}

function run(id) {
  return {
    id,
    name: "phenix.refine",
    software_name: "phenix.refine",
    software_version: "1.21.2",
    command: null,
    parameters: {},
    metadata: { description: "Refinement" },
    created_by: "user-1",
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
