require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ApiRequestError,
  createEntry,
  createModel,
  decideEntryReview,
  getEntry,
  getEntryReview,
  getModelPageData,
  listEntries,
  listModels,
  listReviews,
  listUserEntryRevisionGroups,
  submitModelRevision,
} = require("../src/lib/api/entries.ts");
const { REVIEW_PAGE_SIZE } = require("../src/lib/reviewQueue.ts");

const jsonApiMediaType = "application/vnd.api+json";

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
      return jsonResponse(collectionDocument("entries", [{ id: "entry-1", name: "Entry 1" }]));
    };

    const entries = await listEntries("token-123", { query: "  hemoglobin  " });

    assert.equal(requestedURL, "https://backend.example/v1/entries?query=hemoglobin");
    assert.equal(requestedHeaders.Accept, jsonApiMediaType);
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(entries, [{ id: "entry-1", name: "Entry 1" }]);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should list models with pagination and optional authorization", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    let requestedHeaders = {};
    global.fetch = async (url, init) => {
      requestedURL = String(url);
      requestedHeaders = init.headers;
      return jsonResponse(collectionDocument("models", [{ id: "model-1", name: "Model 1" }]));
    };

    const models = await listModels("token-123", {
      limit: 5,
      offset: 10,
    });

    assert.equal(
      requestedURL,
      "https://backend.example/v1/models?limit=5&offset=10",
    );
    assert.equal(requestedHeaders.Accept, jsonApiMediaType);
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(models, [{ id: "model-1", name: "Model 1" }]);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should get entry from json api document", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    let requestedHeaders = {};
    global.fetch = async (url, init) => {
      requestedURL = String(url);
      requestedHeaders = init.headers;
      return jsonResponse(entryDocument({
        id: "entry-1",
        created_by: "user-1",
        name: "Entry",
        description: "Description",
        thumbnail_image_url: "https://cdn.example/entry.png",
        metadata: { method: "X-ray crystallography" },
        protein_sequences: [
          {
            id: "sequence-1",
            source_artifact_id: "artifact-1",
            record_index: 0,
            header: "chain A",
            sequence: "ACDE",
            created_at: "2026-01-01T00:00:00Z",
          },
        ],
        published_at: null,
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-02T00:00:00Z",
      }));
    };

    const entry = await getEntry("token-123", "entry-1");

    assert.equal(requestedURL, "https://backend.example/v1/entries/entry-1");
    assert.equal(requestedHeaders.Accept, jsonApiMediaType);
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(entry, {
      id: "entry-1",
      created_by: "user-1",
      name: "Entry",
      description: "Description",
      thumbnail_image_url: "https://cdn.example/entry.png",
      metadata: { method: "X-ray crystallography" },
      protein_sequences: [
        {
          id: "sequence-1",
          source_artifact_id: "artifact-1",
          record_index: 0,
          header: "chain A",
          sequence: "ACDE",
          created_at: "2026-01-01T00:00:00Z",
        },
      ],
      published_at: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-02T00:00:00Z",
    });
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should list entries with pdb id filters", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    global.fetch = async (url) => {
      requestedURL = String(url);
      return jsonResponse(collectionDocument("entries", []));
    };

    await listEntries(undefined, { pdbIds: [" 1YJO ", "1YJP"] });

    assert.equal(requestedURL, "https://backend.example/v1/entries?pdb_id=1YJO&pdb_id=1YJP");
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
      return jsonResponse(
        resourceDocument(
          "entry_revision_results",
          {
          entry_id: "entry-1",
          revision_id: "entry-revision-1",
          state: "in_review",
          model_results: [
            {
              op: "add",
              model_id: "model-1",
              model_revision_id: "model-revision-1",
            },
          ],
        },
        ),
        { status: 201 },
      );
    };

    const result = await createEntry("token-123", {
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
          name: "Model",
          entities: [
            {
              id: "model-artifact",
              type: "model",
              level: "L2",
              name: "model.cif",
              payload: {
                file_url: "s3://dynamic-pdb/model.cif",
                sha256: "ABCDEF123456",
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

    assert.equal(result.entry_id, "entry-1");
    assert.equal(result.revision_id, "entry-revision-1");
    assert.equal(result.model_results[0].model_revision_id, "model-revision-1");
    assert.equal(request.url, "https://backend.example/v1/entries");
    assert.equal(request.init.headers.Accept, jsonApiMediaType);
    assert.equal(request.init.headers["Content-Type"], jsonApiMediaType);
    assert.equal(request.init.headers.Authorization, "Bearer token-123");
    assert.equal(request.body.entities, undefined);
    assert.deepEqual(request.body.entry.artifacts, [
      {
        id: "baseline",
        name: "sequence.fasta",
        level: "L0",
        type: "fasta",
        uri: "s3://dynamic-pdb/sequence.fasta",
        sha256: null,
        format: "fasta",
        size_bytes: 12,
        metadata: { records: [{ header: "A", sequence: "AC" }] },
      },
    ]);
    assert.equal("id" in request.body.entry, false);
    assert.equal(request.body.model_operations[0].op, "add");
    assert.equal("model_id" in request.body.model_operations[0].data, false);
    assert.equal(
      request.body.model_operations[0].data.idempotency_key,
      "abcdef123456",
    );
    assert.equal(
      request.body.model_operations[0].data.primary_artifact_id,
      "model-artifact",
    );
    assert.deepEqual(request.body.model_operations[0].data.metadata, {
      authors: ["Alice"],
      affiliation: "Lab",
    });
    assert.deepEqual(request.body.model_operations[0].data.runs[0].artifacts, [
      { artifact_id: "baseline", direction: "input", position: null },
      { artifact_id: "model-artifact", direction: "output", position: null },
    ]);
    assert.match(
      request.body.model_operations[0].data.metrics[0].id,
      /^[0-9a-f-]{36}$/,
    );
    assert.deepEqual(
      {
        key: request.body.model_operations[0].data.metrics[0].key,
        value: request.body.model_operations[0].data.metrics[0].value,
      },
      { key: "r_free", value: 0.23 },
    );
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should create and submit model revisions through user routes", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const requests = [];
    global.fetch = async (url, init) => {
      const request = {
        url: String(url),
        method: init.method,
        headers: init.headers,
        body: JSON.parse(init.body),
      };
      requests.push(request);
      if (request.method === "POST") {
        return jsonResponse(
          resourceDocument(
            "model_revision_results",
            {
            entry_id: "entry-1",
            model_id: "model-1",
            revision_id: "model-revision-1",
            state: "in_review",
          },
          ),
          { status: 201 },
        );
      }
      return jsonResponse(resourceDocument("entry_revisions", entryRevision("entry-revision-1", "2026-01-01T00:00:00Z")));
    };

    const result = await createModel("token-123", "entry-1", {
      name: "Model",
    });
    await submitModelRevision(
      "token-123",
      "user-1",
      result.entry_id,
      result.model_id,
      result.revision_id,
    );

    assert.equal(requests[0].url, "https://backend.example/v1/entries/entry-1/models");
    assert.deepEqual(requests[0].body.model, {
      name: "Model",
      idempotency_key: null,
      metadata: {},
      primary_artifact_id: null,
      artifacts: [],
      runs: [],
      metrics: [],
    });
    assert.equal(
      requests[1].url,
      "https://backend.example/v1/users/user-1/entries/entry-1/models/model-1/revisions/model-revision-1",
    );
    assert.deepEqual(requests.slice(1).map((request) => request.body), [
      { state: "in_review" },
    ]);
    assert.deepEqual(
      requests.map((request) => request.headers.Accept),
      [jsonApiMediaType, jsonApiMediaType],
    );
    assert.deepEqual(
      requests.map((request) => request.headers["Content-Type"]),
      [jsonApiMediaType, jsonApiMediaType],
    );
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should list user revision groups by explicit state", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    let requestedURL = "";
    global.fetch = async (url) => {
      requestedURL = String(url);
      return jsonResponse(collectionDocument("entry_revision_groups", [{ entry: { id: "entry-1" } }]));
    };

    const groups = await listUserEntryRevisionGroups(
      "token-123",
      "user-1",
      "in_review",
    );

    assert.equal(
      requestedURL,
      "https://backend.example/v1/users/user-1/entries/revisions?state=in_review",
    );
    assert.equal(groups[0].entry.id, "entry-1");
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should group the review queue by entry and pair each revision with the one it replaces", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const paths = [];
    global.fetch = async (url) => {
      const requested = new URL(String(url));
      paths.push(`${requested.pathname}${requested.search}`);
      if (requested.pathname === "/v1/entries/revisions") {
        return jsonResponse(collectionDocument(
          "entry_revision_groups",
          [
            {
              entry: {
                id: "entry-1",
                name: "Entry",
                metadata: { external_refs: { pdb: "7B3H" } },
              },
              entry_revisions: [
                revisionSummary("entry-revision-1", "2026-01-01T01:00:00Z"),
              ],
              model_revisions: [
                modelRevisionSummary(
                  "model-revision-1",
                  "model-1",
                  "2026-01-01T02:00:00Z",
                ),
              ],
            },
          ],
        ));
      }
      if (requested.pathname.endsWith("/revisions/entry-revision-1")) {
        return jsonResponse(
          resourceDocument(
            "entry_revisions",
            entryRevision("entry-revision-1", "2026-01-01T01:00:00Z", {
              description: "Fresh",
            }),
          ),
        );
      }
      if (requested.pathname.endsWith("/revisions/model-revision-1")) {
        return jsonResponse(
          resourceDocument(
            "model_revisions",
            modelRevision("model-revision-1", "2026-01-01T02:00:00Z", {
              parent_revision_id: "model-revision-active",
              description: "Updated",
            }),
          ),
        );
      }
      if (requested.pathname.endsWith("/revisions/model-revision-active")) {
        return jsonResponse(
          resourceDocument(
            "model_revisions",
            modelRevision("model-revision-active", "2025-12-01T00:00:00Z", {
              state: "active",
              description: "Original",
            }),
          ),
        );
      }
      throw new Error(`unexpected fetch ${url}`);
    };

    const page = await listReviews("token-123");
    const review = await getEntryReview("token-123", page.items[0]);

    assert.equal(
      paths[0],
      `/v1/entries/revisions?state=in_review&limit=${REVIEW_PAGE_SIZE}&offset=0`,
    );
    // One row per entry, never one per revision.
    assert.equal(page.items.length, 1);
    assert.equal(page.hasMore, false);
    assert.equal(page.items[0].name, "PDB 7B3H | entry-1");
    // The row is stamped with the earliest thing waiting under it.
    assert.equal(page.items[0].submitted_at, "2026-01-01T01:00:00Z");

    // A first revision has nothing published to compare against.
    assert.equal(review.entry.active, null);
    assert.equal(review.entry.proposed.description, "Fresh");
    assert.deepEqual(review.entry.target, {
      kind: "entry",
      entry_id: "entry-1",
      revision_id: "entry-revision-1",
    });

    assert.equal(review.models.length, 1);
    assert.equal(review.models[0].model_id, "model-1");
    assert.equal(review.models[0].active.description, "Original");
    assert.equal(review.models[0].proposed.description, "Updated");
    assert.deepEqual(review.models[0].target, {
      kind: "model",
      entry_id: "entry-1",
      model_id: "model-1",
      revision_id: "model-revision-1",
    });
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should decide the entry revision and each model revision at its own path", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const requests = [];
    global.fetch = async (url, init = {}) => {
      requests.push({
        path: new URL(String(url)).pathname,
        method: init.method,
        headers: init.headers,
        body: init.body,
      });
      return jsonResponse(resourceDocument("entry_revisions", entryRevision("entry-revision-1", "2026-01-01T00:00:00Z", { state: "active" })));
    };

    await decideEntryReview(
      "token-123",
      { kind: "entry", entry_id: "entry-1", revision_id: "entry-revision-1" },
      "active",
    );
    await decideEntryReview(
      "token-123",
      {
        kind: "model",
        entry_id: "entry-1",
        model_id: "model-1",
        revision_id: "model-revision-1",
      },
      "rejected",
    );

    assert.deepEqual(requests, [
      {
        path: "/v1/entries/entry-1/revisions/entry-revision-1",
        method: "PATCH",
        headers: {
          Accept: jsonApiMediaType,
          "Content-Type": jsonApiMediaType,
          Authorization: "Bearer token-123",
        },
        body: JSON.stringify({ state: "active" }),
      },
      {
        path: "/v1/entries/entry-1/models/model-1/revisions/model-revision-1",
        method: "PATCH",
        headers: {
          Accept: jsonApiMediaType,
          "Content-Type": jsonApiMediaType,
          Authorization: "Bearer token-123",
        },
        body: JSON.stringify({ state: "rejected" }),
      },
    ]);
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
        return jsonResponse(entryDocument(entry));
      }
      if (path === "/v1/entries/entry-1/models/model-1") {
        return jsonResponse(resourceDocument("models", model));
      }
      if (path === "/v1/entries/entry-1/models/model-1/artifacts") {
        return jsonResponse(modelArtifactDocument(
          [artifact("model-artifact", "model.cif", "L2", "mmcif")],
          [run("program-1")],
          [
            { run_id: "program-1", artifact_id: "baseline", direction: "input", position: null },
            { run_id: "program-1", artifact_id: "model-artifact", direction: "output", position: null },
          ],
        ));
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

function revisionSummary(id, updatedAt) {
  return {
    id,
    entry_id: "entry-1",
    parent_revision_id: null,
    revision_number: null,
    state: "in_review",
    entry_state: "active",
    created_by: "user-1",
    name: "Entry",
    published_at: null,
    created_at: updatedAt,
    updated_at: updatedAt,
  };
}

function modelRevisionSummary(id, modelId, updatedAt) {
  return {
    id,
    entry_id: "entry-1",
    model_id: modelId,
    parent_revision_id: null,
    revision_number: null,
    state: "in_review",
    model_state: "active",
    created_by: "user-1",
    name: "Model",
    published_at: null,
    created_at: updatedAt,
    updated_at: updatedAt,
  };
}

function jsonResponse(body, init = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": jsonApiMediaType },
    ...init,
  });
}

function resourceDocument(type, attributes) {
  return {
    data: {
      type,
      id: attributes.id ?? attributes.revision_id ?? attributes.model_revision_id,
      attributes,
    },
  };
}

function collectionDocument(type, items) {
  return {
    data: items.map((attributes) => ({
      type,
      id: attributes.id ?? attributes.entry?.id,
      attributes,
    })),
  };
}

function modelArtifactDocument(artifacts, runs, relations) {
  return {
    data: artifacts.map((attributes) => ({
      type: "artifacts",
      id: attributes.id,
      attributes,
    })),
    meta: { runs, relations },
  };
}

function entryDocument(entry) {
  const proteinSequences = entry.protein_sequences ?? [];
  return {
    data: {
      type: "entries",
      id: entry.id,
      attributes: {
        name: entry.name,
        description: entry.description,
        thumbnail_image_url: entry.thumbnail_image_url,
        metadata: entry.metadata ?? {},
        published_at: entry.published_at ?? null,
        created_at: entry.created_at,
        updated_at: entry.updated_at,
      },
      relationships: {
        created_by: {
          data: { type: "users", id: entry.created_by ?? "user-1" },
        },
        protein_sequences: {
          data: proteinSequences.map((sequence) => ({
            type: "protein_sequences",
            id: sequence.id,
          })),
        },
      },
    },
    included: proteinSequences.map((sequence) => ({
      type: "protein_sequences",
      id: sequence.id,
      attributes: {
        record_index: sequence.record_index,
        header: sequence.header,
        sequence: sequence.sequence,
        created_at: sequence.created_at,
      },
      relationships: {
        source_artifact: {
          data: {
            type: "artifacts",
            id: sequence.source_artifact_id,
          },
        },
      },
    })),
  };
}

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}

function entryRevision(id, updatedAt, overrides = {}) {
  return {
    ...revisionSummary(id, updatedAt),
    description: null,
    thumbnail_image_url: null,
    metadata: {},
    protein_sequences: [],
    artifacts: [],
    ...overrides,
  };
}

function modelRevision(id, updatedAt, overrides = {}) {
  return {
    ...modelRevisionSummary(id, "model-1", updatedAt),
    description: null,
    thumbnail_image_url: null,
    primary_artifact_id: null,
    metadata: {},
    metrics: [],
    artifacts: [],
    ...overrides,
  };
}
