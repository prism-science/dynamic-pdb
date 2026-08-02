require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ApiRequestError,
  createStructure,
  getModelPageData,
  listStructures,
} = require("../src/lib/api/structures.ts");

test("should list structures with trimmed query and optional authorization", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    let requestedHeaders = {};
    global.fetch = async (url, init) => {
      requestedURL = String(url);
      requestedHeaders = init.headers;
      return jsonResponse({ items: [{ id: "structure-1", name: "Structure 1" }] });
    };

    const structures = await listStructures("token-123", { query: "  hemoglobin  " });

    assert.equal(requestedURL, "https://backend.example/v1/structures?query=hemoglobin");
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(structures, [{ id: "structure-1", name: "Structure 1" }]);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should throw ApiRequestError with status when create structure fails", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    global.fetch = async () => new Response(JSON.stringify({ code: "BAD_REQUEST" }), { status: 400 });

    await assert.rejects(
      () => createStructure("token-123", { name: "Structure" }),
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

test("should scope model page data to model entities and L0 baseline entities", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const structure = {
      id: "structure-1",
      name: "Structure",
      description: null,
      thumbnail_image_url: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const model = {
      id: "model-1",
      structure_id: "structure-1",
      name: "Model",
      description: null,
      thumbnail_image_url: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const baseline = entity("baseline", null, "data", "L0");
    const modelEntity = entity("model", "model-1", "model", "L2");
    const otherModel = entity("other-model", "model-2", "model", "L2");
    const relations = [
      relation("r1", "baseline", "model", "input_to"),
      relation("r2", "other-model", "baseline", "input_to"),
      relation("r3", "other-model", "missing", "output_of"),
    ];
    global.fetch = async (url) => {
      const path = new URL(String(url)).pathname;
      if (path === "/v1/structures/structure-1") {
        return jsonResponse(structure);
      }
      if (path === "/v1/structures/structure-1/models/model-1") {
        return jsonResponse(model);
      }
      if (path === "/v1/structures/structure-1/entities") {
        return jsonResponse({ items: [baseline, modelEntity, otherModel], relations });
      }
      throw new Error(`unexpected fetch ${url}`);
    };

    const data = await getModelPageData("token-123", "structure-1", "model-1");

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

function entity(id, modelId, type, level) {
  return {
    id,
    structure_id: "structure-1",
    model_id: modelId,
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
