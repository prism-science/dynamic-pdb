require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  ApiRequestError,
  createEntry,
  createModel,
  decideEntryReview,
  findReviewItem,
  getEntry,
  getEntryPageData,
  getEntryReview,
  getModel,
  getModelPageData,
  listEntries,
  listEntriesPage,
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
      return jsonResponse(collectionDocument("entries", [{ id: "entry-1", title: "Entry 1" }]));
    };

    const entries = await listEntries("token-123", { query: "  hemoglobin  " });

    assert.equal(requestedURL, "https://backend.example/v1/entries?query=hemoglobin");
    assert.equal(requestedHeaders.Accept, jsonApiMediaType);
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(entries, [{ id: "entry-1", title: "Entry 1" }]);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should list models with a limit and optional authorization", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    let requestedHeaders = {};
    global.fetch = async (url, init) => {
      requestedURL = String(url);
      requestedHeaders = init.headers;
      return jsonResponse(collectionDocument("models", [{ id: "model-1", title: "Model 1" }]));
    };

    const models = await listModels("token-123", { limit: 5 });

    assert.equal(requestedURL, "https://backend.example/v1/models?limit=5");
    assert.equal(requestedHeaders.Accept, jsonApiMediaType);
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(models, [{ id: "model-1", title: "Model 1" }]);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should send the cursor and return the next cursor when listing an entries page", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    global.fetch = async (url) => {
      requestedURL = String(url);
      return jsonResponse({
        ...collectionDocument("entries", [{ id: "entry-2", title: "Entry 2" }]),
        links: { next: "https://backend.example/v1/entries?cursor=next-cursor" },
        meta: { next_cursor: "next-cursor" },
      });
    };

    const page = await listEntriesPage(undefined, { query: "kinase", limit: 1, cursor: "a+b/c" });

    assert.equal(requestedURL, "https://backend.example/v1/entries?query=kinase&limit=1&cursor=a%2Bb%2Fc");
    assert.deepEqual(page.items, [{ id: "entry-2", title: "Entry 2" }]);
    assert.equal(page.nextCursor, "next-cursor");
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should report no next cursor on the last entries page", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    global.fetch = async () => jsonResponse(collectionDocument("entries", [{ id: "entry-1" }]));

    const page = await listEntriesPage(undefined, { limit: 1 });

    assert.equal(page.nextCursor, null);
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
        title: "Entry",
        thumbnail_image_url: "https://cdn.example/entry.png",
        method: "X-ray crystallography",
        details: "Additional structure details",
        crystallography: {
          crystals: [{
            id: "1",
            growth: { ph: 7.5, temperature_kelvin: 293 },
            diffractions: [{ id: "1", temperature_kelvin: 100 }],
          }],
        },
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
        polymer_entities: [
          {
            id: "polymer-1",
            label_entity_id: "1",
            description: "Hemoglobin subunit alpha",
            source_organisms: [
              { scientific_name: "Homo sapiens", ncbi_taxonomy_id: 9606 },
            ],
            uniprot_mappings: [
              { accession: "P69905", source: "sifts", unp_release: "2025_03" },
            ],
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
      title: "Entry",
      thumbnail_image_url: "https://cdn.example/entry.png",
      method: "X-ray crystallography",
      details: "Additional structure details",
      crystallography: {
        crystals: [{
          id: "1",
          growth: { ph: 7.5, temperature_kelvin: 293 },
          diffractions: [{ id: "1", temperature_kelvin: 100 }],
        }],
      },
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
      polymer_entities: [
        {
          id: "polymer-1",
          label_entity_id: "1",
          description: "Hemoglobin subunit alpha",
          source_organisms: [
            { scientific_name: "Homo sapiens", ncbi_taxonomy_id: 9606 },
          ],
          uniprot_mappings: [
            { accession: "P69905", source: "sifts", unp_release: "2025_03" },
          ],
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
      () => createEntry("token-123", { title: "Entry" }),
      (error) =>
        error instanceof ApiRequestError &&
        error.status === 400 &&
        error.message === "Backend responded with 400" &&
        error.errors.length === 0,
    );
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should expose field errors when backend rejects create entry with 422", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    // given
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    global.fetch = async () =>
      jsonResponse(
        {
          errors: [
            {
              status: "422",
              code: "invalid_value",
              detail: "metric key must be one of the allowed keys",
              source: { pointer: "/model_operations/0/data/metrics/1/key" },
            },
            { status: "422", detail: "title is required", source: { pointer: "/entry/title" } },
          ],
        },
        { status: 422 },
      );

    // when
    const rejection = createEntry("token-123", { title: "Entry" });

    // then
    await assert.rejects(rejection, (error) => {
      assert.ok(error instanceof ApiRequestError);
      assert.equal(error.status, 422);
      assert.equal(error.errors.length, 2);
      assert.equal(error.errors[0].detail, "metric key must be one of the allowed keys");
      assert.equal(error.errors[0].source.pointer, "/model_operations/0/data/metrics/1/key");
      assert.equal(error.errors[0].code, "invalid_value");
      assert.equal(
        error.message,
        "Backend responded with 422: metric key must be one of the allowed keys (/model_operations/0/data/metrics/1/key); title is required (/entry/title)",
      );
      return true;
    });
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
      title: "Entry",
      external_refs: { pdb: "5GY3" },
      details: "Additional structure details",
      resolution: 1.77,
      method: "X-ray crystallography",
      space_group: "P 21 21 21",
      crystallography: {
        crystals: [{
          id: "1",
          growth: { ph: 9, temperature_kelvin: 293 },
          diffractions: [{ id: "1", temperature_kelvin: 100 }],
        }],
      },
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
          title: "Model",
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
                metadata: {
                  details: "Model details",
                  altloc_fraction: 0.25,
                  unmodeled_fraction: 0.1,
                  cofactors: ["HEM"],
                },
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
    assert.equal(request.body.entry.metadata, undefined);
    assert.deepEqual(request.body.entry.external_refs, { pdb: "5GY3" });
    assert.equal(request.body.entry.details, "Additional structure details");
    assert.equal(request.body.entry.resolution, 1.77);
    assert.equal(request.body.entry.method, "X-ray crystallography");
    assert.equal(request.body.entry.space_group, "P 21 21 21");
    assert.equal(request.body.entry.crystallography.crystals[0].growth.ph, 9);
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
      details: "Model details",
      altloc_fraction: 0.25,
      unmodeled_fraction: 0.1,
      cofactors: ["HEM"],
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

test("should send only declared artifact metadata keys when files carry parsed and ext metadata", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    // given
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    let request = null;
    global.fetch = async (url, init) => {
      request = { body: JSON.parse(init.body) };
      return jsonResponse(
        resourceDocument("entry_revision_results", {
          entry_id: "entry-1",
          revision_id: "entry-revision-1",
          state: "in_review",
          model_results: [],
        }),
        { status: 201 },
      );
    };
    const extSource = {
      provider: "ext",
      experiment_id: "experiment-1",
      path: "data/refine.mtz",
      sha256: "abc123",
    };

    // when
    await createEntry("token-123", {
      title: "Entry",
      method: "X-ray crystallography",
      models: [
        {
          title: "Model",
          entities: [
            {
              id: "model-artifact",
              type: "model",
              level: "L2",
              name: "model.cif",
              payload: {
                file_url: "s3://dynamic-pdb/model.cif",
                metadata: {
                  atom_count: 2400,
                  modeled_residues: 300,
                  unique_protein_chains: 1,
                  altloc_fraction: 0.1,
                  ligands: ["ATP"],
                  method: "X-RAY DIFFRACTION",
                  resolution: 1.8,
                  space_group: "P 21 21 21",
                  header_note: "from the file header",
                },
              },
            },
            {
              id: "ext-artifact",
              type: "data",
              level: "L2",
              name: "refine.mtz",
              payload: {
                file_url: "https://ext.example/refine.mtz",
                metadata: {
                  _dynamic_pdb_source: extSource,
                  resolution: 1.9,
                  beamline: "ALS 8.3.1",
                  __internal: true,
                  detector: { model: "Pilatus", __serial: "X1" },
                },
              },
            },
          ],
        },
      ],
    });

    // then
    const data = request.body.model_operations[0].data;
    const [modelArtifact, extArtifact] = data.artifacts;
    assert.deepEqual(modelArtifact.metadata, {
      extensions: { header_note: "from the file header" },
    });
    assert.deepEqual(extArtifact.metadata, {
      _dynamic_pdb_source: extSource,
      extensions: {
        ext_metadata: {
          resolution: 1.9,
          beamline: "ALS 8.3.1",
          detector: { model: "Pilatus" },
        },
      },
    });
    assert.deepEqual(data.metadata, {
      atom_count: 2400,
      modeled_residues: 300,
      unique_protein_chains: 1,
      altloc_fraction: 0.1,
      ligands: ["ATP"],
    });
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should keep existing artifact extensions when undeclared metadata keys move into them", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    // given
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    let request = null;
    global.fetch = async (url, init) => {
      request = { body: JSON.parse(init.body) };
      return jsonResponse(
        resourceDocument("entry_revision_results", {
          entry_id: "entry-1",
          revision_id: "entry-revision-1",
          state: "in_review",
          model_results: [],
        }),
        { status: 201 },
      );
    };

    // when
    await createEntry("token-123", {
      title: "Entry",
      method: "X-ray crystallography",
      entities: [
        {
          id: "notes",
          type: "data",
          level: "L0",
          name: "notes.txt",
          payload: {
            file_url: "s3://dynamic-pdb/notes.txt",
            metadata: {
              extensions: { label: "kept" },
              label: "moved",
              source: "bench",
            },
          },
        },
      ],
    });

    // then
    assert.deepEqual(request.body.entry.artifacts[0].metadata, {
      extensions: { label: "kept", source: "bench" },
    });
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
      title: "Model",
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
      title: "Model",
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
                title: "Entry",
                external_refs: { pdb: "7B3H" },
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
              title: "Fresh",
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
              title: "Updated",
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
              title: "Original",
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
      `/v1/entries/revisions?state=in_review&limit=${REVIEW_PAGE_SIZE}`,
    );
    // One row per entry, never one per revision.
    assert.equal(page.items.length, 1);
    assert.equal(page.nextCursor, null);
    assert.equal(page.items[0].title, "PDB 7B3H | entry-1");
    // The row is stamped with the earliest thing waiting under it.
    assert.equal(page.items[0].submitted_at, "2026-01-01T01:00:00Z");

    // A first revision has nothing published to compare against.
    assert.equal(review.entry.active, null);
    assert.equal(review.entry.proposed.title, "Fresh");
    assert.deepEqual(review.entry.target, {
      kind: "entry",
      entry_id: "entry-1",
      revision_id: "entry-revision-1",
    });

    assert.equal(review.models.length, 1);
    assert.equal(review.models[0].model_id, "model-1");
    assert.equal(review.models[0].active.title, "Original");
    assert.equal(review.models[0].proposed.title, "Updated");
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

test("should get model residue data from the model detail endpoint", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const residueData = [{ label_asym_id: "A", label_seq_id: 52, rscc: 0.97 }];
    let requestedURL = "";
    global.fetch = async (url) => {
      requestedURL = String(url);
      return jsonResponse(resourceDocument("models", { id: "model 1", residue_data: residueData }));
    };

    const model = await getModel("token-123", "entry/1", "model 1");

    assert.equal(requestedURL, "https://backend.example/v1/entries/entry%2F1/models/model%201");
    assert.deepEqual(model.residue_data, residueData);
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
      title: "Entry",
      thumbnail_image_url: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const model = {
      id: "model-1",
      entry_id: "entry-1",
      title: "Model",
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

test("should walk the review queue by cursor until the requested entry turns up", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const queries = [];
    global.fetch = async (url) => {
      const requested = new URL(String(url));
      queries.push(requested.search);
      const onSecondPage = requested.searchParams.get("cursor") === "after-entry-1";
      const document = collectionDocument("entry_revision_groups", [
        reviewGroup(onSecondPage ? "entry-2" : "entry-1"),
      ]);
      if (!onSecondPage) {
        document.meta = { next_cursor: "after-entry-1" };
      }
      return jsonResponse(document);
    };

    const found = await findReviewItem("token-123", "entry-2");
    const missing = await findReviewItem("token-123", "entry-3");

    assert.equal(found.entry_id, "entry-2");
    assert.equal(missing, null);
    assert.deepEqual(queries.slice(0, 2), [
      `?state=in_review&limit=${REVIEW_PAGE_SIZE}`,
      `?state=in_review&limit=${REVIEW_PAGE_SIZE}&cursor=after-entry-1`,
    ]);
    assert.equal(queries.length, 4, "a missing entry stops at the page without a next cursor");
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

function reviewGroup(entryId) {
  return {
    entry: { id: entryId, title: entryId },
    entry_revisions: [revisionSummary(`${entryId}-revision`, "2026-01-01T01:00:00Z")],
    model_revisions: [],
  };
}

test("should load every page of an entry's models and files when the entry page is built", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const queriesByPath = new Map();
    const relations = [
      { run_id: "program-1", artifact_id: "model-file-1", direction: "output", position: null },
    ];
    global.fetch = async (url) => {
      const requested = new URL(String(url));
      const path = requested.pathname;
      queriesByPath.set(path, [...(queriesByPath.get(path) ?? []), requested.search]);
      const cursor = requested.searchParams.get("cursor");
      if (path === "/v1/entries/entry-1") {
        return jsonResponse(entryDocument(pagingEntry()));
      }
      if (path === "/v1/entries/entry-1/models") {
        return jsonResponse(cursor === null
          ? withNextCursor(collectionDocument("models", [pagingModel("model-1")]), "after-model-1")
          : collectionDocument("models", [pagingModel("model-2")]));
      }
      if (path === "/v1/entries/entry-1/artifacts") {
        return jsonResponse(cursor === null
          ? withNextCursor(collectionDocument("artifacts", [artifact("entry-file-1", "a.fasta", "L0", "fasta")]), "after-entry-file-1")
          : collectionDocument("artifacts", [artifact("entry-file-2", "b.fasta", "L0", "fasta")]));
      }
      if (path === "/v1/entries/entry-1/models/model-1/artifacts") {
        return jsonResponse(cursor === null
          ? withNextCursor(
            modelArtifactDocument([artifact("model-file-1", "model.cif", "L2", "mmcif")], [run("program-1")], relations),
            "after-model-file-1",
          )
          : modelArtifactDocument([artifact("model-file-2", "model.log", "L2", "log")], [run("program-1")], relations));
      }
      if (path === "/v1/entries/entry-1/models/model-2/artifacts") {
        return jsonResponse(modelArtifactDocument([], [], []));
      }
      throw new Error(`unexpected fetch ${url}`);
    };

    const data = await getEntryPageData("token-123", "entry-1");

    assert.deepEqual(data.models.map((model) => model.id), ["model-1", "model-2"]);
    const entityIds = data.entities.map((entity) => entity.id);
    for (const id of ["entry-file-1", "entry-file-2", "model-file-1", "model-file-2"]) {
      assert.ok(entityIds.includes(id), `${id} is on the page`);
    }
    assert.equal(
      entityIds.filter((id) => id === "program-1").length,
      1,
      "runs repeated on every page of a model's files appear once",
    );
    assert.deepEqual(queriesByPath.get("/v1/entries/entry-1/models"), [
      "?limit=100",
      "?limit=100&cursor=after-model-1",
    ]);
    assert.deepEqual(queriesByPath.get("/v1/entries/entry-1/artifacts"), [
      "?limit=100",
      "?limit=100&cursor=after-entry-file-1",
    ]);
    assert.deepEqual(queriesByPath.get("/v1/entries/entry-1/models/model-1/artifacts"), [
      "?limit=100",
      "?limit=100&cursor=after-model-file-1",
    ]);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should stop with an error when a collection keeps handing out cursors", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    let modelPageRequests = 0;
    global.fetch = async (url) => {
      const path = new URL(String(url)).pathname;
      if (path === "/v1/entries/entry-1") {
        return jsonResponse(entryDocument(pagingEntry()));
      }
      if (path === "/v1/entries/entry-1/models") {
        modelPageRequests += 1;
        return jsonResponse(withNextCursor(collectionDocument("models", []), "same-cursor-again"));
      }
      if (path === "/v1/entries/entry-1/artifacts") {
        return jsonResponse(collectionDocument("artifacts", []));
      }
      throw new Error(`unexpected fetch ${url}`);
    };

    await assert.rejects(getEntryPageData("token-123", "entry-1"), ApiRequestError);
    assert.equal(modelPageRequests, 100);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

test("should fetch at most six models' files at once when the entry page is built", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example";
    const modelIds = Array.from({ length: 20 }, (_, index) => `model-${index + 1}`);
    let inFlight = 0;
    let maxInFlight = 0;
    global.fetch = async (url) => {
      const path = new URL(String(url)).pathname;
      if (path === "/v1/entries/entry-1") {
        return jsonResponse(entryDocument(pagingEntry()));
      }
      if (path === "/v1/entries/entry-1/models") {
        return jsonResponse(collectionDocument("models", modelIds.map(pagingModel)));
      }
      if (path === "/v1/entries/entry-1/artifacts") {
        return jsonResponse(collectionDocument("artifacts", []));
      }
      const modelFiles = path.match(/^\/v1\/entries\/entry-1\/models\/([^/]+)\/artifacts$/);
      if (modelFiles) {
        inFlight += 1;
        maxInFlight = Math.max(maxInFlight, inFlight);
        await new Promise((resolve) => setTimeout(resolve, 1));
        inFlight -= 1;
        return jsonResponse(modelArtifactDocument(
          [artifact(`${modelFiles[1]}-file`, "model.cif", "L2", "mmcif")],
          [],
          [],
        ));
      }
      throw new Error(`unexpected fetch ${url}`);
    };

    const data = await getEntryPageData("token-123", "entry-1");

    assert.equal(maxInFlight, 6);
    assert.deepEqual(
      data.entities.map((entity) => entity.id),
      modelIds.map((id) => `${id}-file`),
      "every model's files arrive, in model order",
    );
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
  }
});

function pagingEntry() {
  return {
    id: "entry-1",
    title: "Entry",
    thumbnail_image_url: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function pagingModel(id) {
  return {
    id,
    entry_id: "entry-1",
    title: id,
    thumbnail_image_url: null,
    primary_artifact_id: null,
    metrics: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function withNextCursor(document, nextCursor) {
  return { ...document, meta: { ...document.meta, next_cursor: nextCursor } };
}

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
    title: "Entry",
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
    title: "Model",
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
        title: entry.title,
        thumbnail_image_url: entry.thumbnail_image_url,
        external_refs: entry.external_refs,
        details: entry.details,
        resolution: entry.resolution,
        method: entry.method,
        space_group: entry.space_group,
        crystallography: entry.crystallography,
        polymer_entities: entry.polymer_entities ?? [],
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
    thumbnail_image_url: null,
    primary_artifact_id: null,
    metadata: {},
    metrics: [],
    artifacts: [],
    ...overrides,
  };
}
