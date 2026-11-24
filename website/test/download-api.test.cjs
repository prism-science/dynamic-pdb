require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { ApiRequestError } = require("../src/lib/api/entries.ts");
const {
  bulkDownloadAvailable,
  downloadProblem,
  isUnknownSelection,
  listFailures,
  prepareArchive,
  readArchive,
  retryArchive,
  requestArchive,
  resetBulkDownloadAvailability,
  saveSelection,
} = require("../src/lib/api/downloads.ts");
const {
  prepareArchiveAction,
  retryArchiveAction,
  startDownloadAction,
} = require("../src/app/components/download-selection/actions.ts");
const { GET: getArchiveStatus } = require("../src/app/download/archives/[downloadId]/route.ts");
const { GET: getArchiveFailures } = require("../src/app/download/archives/[downloadId]/failures/route.ts");
const { GET: getSelectionFiles } = require("../src/app/download/selections/[selectionId]/files/route.ts");
const { __setCookieValues } = require("next/headers");
const { authTokenCookieName } = require("../src/lib/auth/session.ts");

const jsonApiMediaType = "application/vnd.api+json";
const downloadId = "0b6e4a52-8f0a-4b8e-9d1c-2f6a3e4b5c6d";
const selectionId = "7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f";

/** Runs body with fetch answered by answer(url, init) and the backend at https://backend.example/. */
async function withBackend(answer, body) {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  const previousInternalURL = process.env.API_INTERNAL_BASE_URL;
  const requests = [];
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    delete process.env.API_INTERNAL_BASE_URL;
    global.fetch = async (url, init = {}) => {
      const request = {
        url: String(url),
        method: init.method ?? "GET",
        headers: init.headers ?? {},
        body: init.body === undefined ? undefined : JSON.parse(init.body),
      };
      requests.push(request);
      return answer(request);
    };
    await body(requests);
  } finally {
    global.fetch = previousFetch;
    restoreEnv("NEXT_PUBLIC_API_BASE_URL", previousApiBaseURL);
    restoreEnv("API_INTERNAL_BASE_URL", previousInternalURL);
    __setCookieValues({});
  }
}

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": jsonApiMediaType } });
}

function errorResponse(status, code, detail = "explained", headers = {}, source = undefined) {
  return new Response(JSON.stringify({ errors: [{ status: String(status), code, detail, ...(source ? { source } : {}) }] }), {
    status,
    headers: { "content-type": jsonApiMediaType, ...headers },
  });
}

/** POST /v1/downloads for a selection ID no saved selection has. */
function unknownSelectionResponse() {
  return errorResponse(422, "INVALID_VALUE", "no saved selection has this ID", {}, { pointer: "/selection_id" });
}

function selectionDocument(id = selectionId) {
  return {
    data: {
      type: "selections",
      id,
      attributes: {
        model_count: 3,
        entry_count: 2,
        created_at: "2026-11-13T10:00:00Z",
        catalog_date: "2026-11-13T10:00:00Z",
        request: { model_ids: ["m1"], searches: [], exclude_model_ids: [], add_deposited_models: true },
      },
      links: { self: "x", models: "y" },
    },
  };
}

function downloadDocument(status = "queued", selection = selectionId) {
  return {
    data: {
      type: "downloads",
      id: downloadId,
      attributes: {
        selection_id: selection,
        file_choice: "all_files",
        status,
        prepare_requested: true,
        created_at: "2026-11-13T10:00:00Z",
        max_archive_bytes: 10_000_000_000,
      },
      links: { self: "x", selection: "y" },
    },
  };
}

const body = { model_ids: ["m1"], searches: ["model_type=Ensemble"], exclude_model_ids: ["m2"], add_deposited_models: true };

test("should save a selection, ask for its archive and read it back with the backend's routes", async () => {
  await withBackend(
    (request) => {
      if (request.url.endsWith("/v1/selections")) return jsonResponse(selectionDocument(), 201);
      if (request.url.endsWith("/v1/downloads")) return jsonResponse(downloadDocument(), 202);
      return jsonResponse(downloadDocument("ready"));
    },
    async (requests) => {
      // when
      const saved = await saveSelection(body, "reader-token");
      const asked = await requestArchive(saved.id, "all_files", true);
      const read = await readArchive(asked.id);

      // then
      assert.deepEqual(saved, { id: selectionId, modelCount: 3, entryCount: 2, createdAt: "2026-11-13T10:00:00Z" });
      assert.equal(asked.attributes.status, "queued");
      assert.equal(read.attributes.status, "ready");
      assert.deepEqual(
        requests.map((request) => [request.method, request.url]),
        [
          ["POST", "https://backend.example/v1/selections"],
          ["POST", "https://backend.example/v1/downloads"],
          ["GET", `https://backend.example/v1/downloads/${downloadId}`],
        ],
      );
      assert.deepEqual(requests[0].body, body);
      assert.equal(requests[0].headers.Authorization, "Bearer reader-token");
      assert.equal(requests[1].headers.Authorization, undefined, "archives need no sign-in");
      assert.deepEqual(requests[1].body, { selection_id: selectionId, file_choice: "all_files", prepare: true });
    },
  );
});

test("should explain each failed call by its status and code", () => {
  // given
  const cases = [
    [new ApiRequestError("x", null), "unreachable"],
    [new ApiRequestError("x", 403, [{ code: "FORBIDDEN" }]), "forbidden"],
    [new ApiRequestError("x", 404, [{ code: "SELECTIONS_OFF" }]), "off"],
    [new ApiRequestError("x", 404, [{ code: "DOWNLOADS_OFF" }]), "off"],
    [new ApiRequestError("x", 404, [{ code: "NOT_FOUND" }]), "not_found"],
    [new ApiRequestError("x", 422, [{ code: "SELECTION_EMPTY" }]), "empty"],
    [new ApiRequestError("x", 422, [{ code: "SELECTION_TOO_LARGE" }]), "too_large"],
    [new ApiRequestError("x", 422, [{ code: "INVALID_VALUE", source: { pointer: "/model_ids/0" } }]), "invalid"],
    [new ApiRequestError("x", 429, [{ code: "RATE_LIMITED" }], 12), "rate_limited"],
    [new ApiRequestError("x", 429, [{ code: "SELECTION_BUSY" }]), "selection_busy"],
    [new ApiRequestError("x", 503, [{ code: "DOWNLOADS_BUSY" }]), "busy"],
    [new ApiRequestError("x", 503, [{ code: "SERVICE_UNAVAILABLE" }]), "busy"],
    [new ApiRequestError("x", 503, [{ code: "SELECTIONS_FULL" }]), "full"],
    [new ApiRequestError("x", 504, [{ code: "TIMEOUT" }]), "timeout"],
    [new ApiRequestError("x", 500, [{ code: "INTERNAL_ERROR" }]), "failed"],
    [new Error("boom"), "failed"],
  ];

  // then
  for (const [error, code] of cases) {
    assert.equal(downloadProblem(error).code, code, `${error.status} ${JSON.stringify(error.errors)}`);
  }
  assert.equal(downloadProblem(cases[8][0]).retryAfterSeconds, 12);
  assert.equal(downloadProblem(new ApiRequestError("x", 504)).code, "timeout", "the website's own fetch timeout");
  assert.equal(
    downloadProblem(new ApiRequestError("x", 403, [{ code: "FORBIDDEN", detail: "flagged needs a reviewer" }])).detail,
    "flagged needs a reviewer",
  );
});

test("should find bulk download on when both routes answer as working routes do for an unknown ID", async () => {
  resetBulkDownloadAvailability();
  await withBackend(
    () => errorResponse(404, "NOT_FOUND"),
    async (requests) => {
      // when
      const available = await bulkDownloadAvailable(1_000);
      const again = await bulkDownloadAvailable(50_000);

      // then
      assert.equal(available, true);
      assert.equal(again, true);
      assert.deepEqual(
        requests.map((request) => request.url).sort(),
        [
          "https://backend.example/v1/downloads/availability-probe",
          "https://backend.example/v1/selections/availability-probe",
        ],
        "asked once a minute at most",
      );
    },
  );
});

test("should find bulk download off when either switch is off", async () => {
  for (const offRoute of ["selections", "downloads"]) {
    resetBulkDownloadAvailability();
    await withBackend(
      (request) =>
        request.url.includes(`/v1/${offRoute}/`)
          ? errorResponse(404, offRoute === "selections" ? "SELECTIONS_OFF" : "DOWNLOADS_OFF")
          : errorResponse(404, "NOT_FOUND"),
      async () => {
        assert.equal(await bulkDownloadAvailable(1_000), false, offRoute);
      },
    );
  }
});

test("should hide bulk download without a clear answer and ask again after 10 seconds", async () => {
  resetBulkDownloadAvailability();
  await withBackend(
    () => errorResponse(500, "INTERNAL_ERROR"),
    async (requests) => {
      // when
      const first = await bulkDownloadAvailable(1_000);
      const soon = await bulkDownloadAvailable(5_000);
      const later = await bulkDownloadAvailable(12_000);

      // then
      assert.equal(first, false);
      assert.equal(soon, false);
      assert.equal(later, false);
      assert.equal(requests.length, 4, "asked at 1 s and again at 12 s");
    },
  );
  resetBulkDownloadAvailability();
});

test("should save the selection as the signed-in reader, then ask for its archive", async () => {
  __setCookieValues({ [authTokenCookieName]: "reviewer-token" });
  await withBackend(
    (request) =>
      request.url.endsWith("/v1/selections")
        ? jsonResponse(selectionDocument(), 201)
        : jsonResponse(downloadDocument(), 202),
    async (requests) => {
      // when
      const result = await startDownloadAction({ selection: body, savedSelectionId: null, fileChoice: "all_files" });

      // then
      assert.equal(result.status, "started");
      assert.equal(result.saved.id, selectionId);
      assert.equal(result.download.id, downloadId);
      assert.equal(requests[0].headers.Authorization, "Bearer reviewer-token");
      assert.deepEqual(requests[1].body, { selection_id: selectionId, file_choice: "all_files", prepare: false });
    },
  );
});

test("should reuse a saved selection, and save anew when the server no longer knows it", async () => {
  // given a reused selection the server knows
  await withBackend(
    () => jsonResponse(downloadDocument("ready"), 200),
    async (requests) => {
      // when
      const result = await startDownloadAction({ selection: body, savedSelectionId: selectionId, fileChoice: "all_files" });

      // then
      assert.equal(result.status, "started");
      assert.equal(result.saved, null);
      assert.deepEqual(requests.map((request) => request.url), ["https://backend.example/v1/downloads"]);
    },
  );
  // given one it does not know
  const freshId = "9e8d7c6b-5a49-4382-9172-6e5d4c3b2a19";
  await withBackend(
    (request) => {
      if (request.url.endsWith("/v1/selections")) return jsonResponse(selectionDocument(freshId), 201);
      return request.body.selection_id === freshId
        ? jsonResponse(downloadDocument("queued", freshId), 202)
        : unknownSelectionResponse();
    },
    async (requests) => {
      // when
      const result = await startDownloadAction({ selection: body, savedSelectionId: selectionId, fileChoice: "all_files" });

      // then
      assert.equal(result.status, "started");
      assert.equal(result.saved.id, freshId);
      assert.deepEqual(
        requests.map((request) => request.url),
        [
          "https://backend.example/v1/downloads",
          "https://backend.example/v1/selections",
          "https://backend.example/v1/downloads",
        ],
      );
    },
  );
});

test("should report why a download could not start", async () => {
  await withBackend(
    () => errorResponse(422, "SELECTION_TOO_LARGE", "narrow the searches"),
    async () => {
      // when
      const result = await startDownloadAction({ selection: body, savedSelectionId: null, fileChoice: "all_files" });

      // then
      assert.deepEqual(result, {
        status: "failed",
        saved: null,
        problem: { code: "too_large", detail: "narrow the searches", retryAfterSeconds: null },
      });
    },
  );
});

test("should refuse a request that is not the panel's without calling the backend", async () => {
  await withBackend(
    () => assert.fail("no backend call expected"),
    async (requests) => {
      // given
      const broken = [
        null,
        { selection: body, savedSelectionId: null, fileChoice: "pdf" },
        { selection: { ...body, model_ids: "m1" }, savedSelectionId: null, fileChoice: "all_files" },
        { selection: { ...body, add_deposited_models: "yes" }, savedSelectionId: null, fileChoice: "all_files" },
        { selection: { ...body, searches: Array(11).fill("a=b") }, savedSelectionId: null, fileChoice: "all_files" },
        { selection: body, savedSelectionId: 7, fileChoice: "all_files" },
      ];

      // then
      for (const input of broken) {
        const result = await startDownloadAction(input);
        assert.equal(result.status, "failed", JSON.stringify(input));
        assert.equal(result.problem.code, "invalid");
      }
      assert.equal(requests.length, 0);
    },
  );
});

test("should pass an archive's status to the panel, and a problem with the backend's status", async () => {
  await withBackend(
    (request) =>
      request.url.endsWith(downloadId) ? jsonResponse(downloadDocument("preparing")) : errorResponse(404, "DOWNLOADS_OFF"),
    async (requests) => {
      // when
      const found = await getArchiveStatus(new Request("http://site/x"), { params: Promise.resolve({ downloadId }) });
      const off = await getArchiveStatus(new Request("http://site/x"), {
        params: Promise.resolve({ downloadId: "1b6e4a52-8f0a-4b8e-9d1c-2f6a3e4b5c6d" }),
      });
      const notAnId = await getArchiveStatus(new Request("http://site/x"), {
        params: Promise.resolve({ downloadId: "../selections" }),
      });

      // then
      assert.equal(found.status, 200);
      assert.equal((await found.json()).download.attributes.status, "preparing");
      assert.equal(off.status, 404);
      assert.equal((await off.json()).problem.code, "off");
      assert.equal(notAnId.status, 404);
      assert.equal(requests.length, 2, "an ID that is not a UUID never reaches the backend");
    },
  );
});

test("should show an error code the panel does not know instead of folding it into a known one", () => {
  // given
  const unknown = [
    new ApiRequestError("x", 503, [{ code: "NEW_ERROR", detail: "something new" }]),
    new ApiRequestError("x", 422, [{ code: "CONSTRAINT_VIOLATION" }]),
    new ApiRequestError("x", 404, [{ code: "GONE_FOR_GOOD" }]),
    new ApiRequestError("x", 429, [{ code: "SLOW_DOWN" }]),
    new ApiRequestError("x", 403, []),
  ];

  // then
  for (const error of unknown) {
    const problem = downloadProblem(error);
    assert.equal(problem.code, "failed", JSON.stringify(error.errors));
    assert.match(problem.detail, new RegExp(`^The server answered ${error.status}`));
  }
  assert.equal(downloadProblem(unknown[0]).detail, "The server answered 503 NEW_ERROR: something new");
});

test("should recognise only a refused selection ID as an unknown selection", () => {
  // then
  assert.equal(
    isUnknownSelection(new ApiRequestError("x", 422, [{ code: "INVALID_VALUE", source: { pointer: "/selection_id" } }])),
    true,
  );
  assert.equal(
    isUnknownSelection(new ApiRequestError("x", 422, [{ code: "INVALID_VALUE", source: { pointer: "/file_choice" } }])),
    false,
  );
  assert.equal(isUnknownSelection(new ApiRequestError("x", 503, [{ code: "DOWNLOADS_BUSY" }])), false);
  assert.equal(isUnknownSelection(new Error("x")), false);
});

test("should keep a saved selection when asking for its archive then fails", async () => {
  await withBackend(
    (request) =>
      request.url.endsWith("/v1/selections")
        ? jsonResponse(selectionDocument(), 201)
        : errorResponse(503, "DOWNLOADS_BUSY", "busy", { "Retry-After": "30" }),
    async () => {
      // when
      const result = await startDownloadAction({ selection: body, savedSelectionId: null, fileChoice: "all_files" });

      // then
      assert.equal(result.status, "failed");
      assert.equal(result.problem.code, "busy");
      assert.equal(result.problem.retryAfterSeconds, 30);
      assert.equal(result.saved.id, selectionId, "the panel keeps it, so a retry reuses it");
    },
  );
});

test("should not save a reused selection again when the archive request fails for another reason", async () => {
  await withBackend(
    () => errorResponse(503, "DOWNLOADS_BUSY", "busy"),
    async (requests) => {
      // when
      const result = await startDownloadAction({ selection: body, savedSelectionId: selectionId, fileChoice: "all_files" });

      // then
      assert.equal(result.status, "failed");
      assert.equal(result.saved, null);
      assert.deepEqual(requests.map((request) => request.url), ["https://backend.example/v1/downloads"]);
    },
  );
});

test("should hide bulk download when the probe gets anything but the API's own 404s", async () => {
  // given answers a working route never gives for an unknown ID
  const answers = [
    () => new Response("<html>not found</html>", { status: 404, headers: { "content-type": "text/html" } }),
    () => jsonResponse({ data: {} }, 200),
    () => errorResponse(404, "SOMETHING_ELSE"),
  ];
  for (const answer of answers) {
    resetBulkDownloadAvailability();
    await withBackend(answer, async () => {
      // then
      assert.equal(await bulkDownloadAvailable(1_000), false);
    });
  }
  resetBulkDownloadAvailability();
});

test("should build, retry and page the failures of an archive with the backend's routes", async () => {
  await withBackend(
    (request) => {
      if (request.url.includes("/failures")) {
        return jsonResponse({
          data: [
            {
              type: "download_failures",
              id: "f1",
              attributes: {
                file_id: "f1",
                file_name: "model.pdb",
                entry_id: "dpdb_e1",
                model_id: "dpdb_e1_m1",
                status: "unavailable",
                reason_code: "read_failed",
                reason: "our copy could not be read",
                retryable: true,
              },
            },
          ],
          meta: { next_cursor: "c2" },
        });
      }
      return jsonResponse(downloadDocument("queued"), 202);
    },
    async (requests) => {
      // when
      const prepared = await prepareArchive(downloadId);
      const retried = await retryArchive(downloadId);
      const page = await listFailures(downloadId, "c1");

      // then
      assert.equal(prepared.id, downloadId);
      assert.equal(retried.id, downloadId);
      assert.deepEqual(page.items.map((item) => [item.file_name, item.reason_code, item.retryable]), [
        ["model.pdb", "read_failed", true],
      ]);
      assert.equal(page.nextCursor, "c2");
      assert.deepEqual(
        requests.map((request) => [request.method, request.url]),
        [
          ["POST", `https://backend.example/v1/downloads/${downloadId}/prepare`],
          ["POST", `https://backend.example/v1/downloads/${downloadId}/retry`],
          ["GET", `https://backend.example/v1/downloads/${downloadId}/failures?limit=20&cursor=c1`],
        ],
      );
    },
  );
});

test("should explain why an archive cannot be built or retried", () => {
  // then
  assert.equal(downloadProblem(new ApiRequestError("x", 409, [{ code: "DOWNLOAD_NOT_PREPARABLE" }])).code, "not_preparable");
  assert.equal(downloadProblem(new ApiRequestError("x", 409, [{ code: "NOTHING_TO_RETRY" }])).code, "nothing_to_retry");
  assert.equal(downloadProblem(new ApiRequestError("x", 409, [{ code: "DOWNLOAD_NOT_RETRYABLE" }])).code, "not_retryable");
  assert.equal(downloadProblem(new ApiRequestError("x", 409, [{ code: "CONFLICT" }])).code, "failed");
});

test("should build and retry through the actions, and refuse an ID that is not an archive's", async () => {
  await withBackend(
    (request) =>
      request.url.endsWith("/retry")
        ? errorResponse(409, "NOTHING_TO_RETRY", "nothing a retry may fetch")
        : jsonResponse(downloadDocument("planned"), 202),
    async (requests) => {
      // when
      const prepared = await prepareArchiveAction(downloadId);
      const retried = await retryArchiveAction(downloadId);
      const refused = await prepareArchiveAction("../selections");

      // then
      assert.equal(prepared.status, "done");
      assert.equal(prepared.download.id, downloadId);
      assert.deepEqual(retried, {
        status: "failed",
        problem: { code: "nothing_to_retry", detail: "nothing a retry may fetch", retryAfterSeconds: null },
      });
      assert.equal(refused.status, "failed");
      assert.equal(refused.problem.code, "invalid");
      assert.equal(requests.length, 2, "an ID that is not a UUID never reaches the backend");
    },
  );
});

test("should pass a page of failures to the panel, and a problem with the backend's status", async () => {
  await withBackend(
    (request) =>
      request.url.includes(downloadId) ? jsonResponse({ data: [], meta: {} }) : errorResponse(404, "NOT_FOUND"),
    async (requests) => {
      // when
      const found = await getArchiveFailures(new Request("http://site/x?cursor=c9"), {
        params: Promise.resolve({ downloadId }),
      });
      const unknown = await getArchiveFailures(new Request("http://site/x"), {
        params: Promise.resolve({ downloadId: "1b6e4a52-8f0a-4b8e-9d1c-2f6a3e4b5c6d" }),
      });
      const notAnId = await getArchiveFailures(new Request("http://site/x"), {
        params: Promise.resolve({ downloadId: "nope" }),
      });

      // then
      assert.equal(found.status, 200);
      assert.deepEqual(await found.json(), { items: [], nextCursor: null });
      assert.match(requests[0].url, /cursor=c9/);
      assert.equal(unknown.status, 404);
      assert.equal((await unknown.json()).problem.code, "not_found");
      assert.equal(notAnId.status, 404);
      assert.equal(requests.length, 2);
    },
  );
});

test("should pass a page of a saved selection's files with their last check, and a problem with the backend's status", async () => {
  const page = {
    data: [
      {
        type: "selection-models",
        id: "m1",
        attributes: {
          model_id: "m1",
          entry_id: "e1",
          model_revision_number: 1,
          model_type: "Ensemble",
          origin: null,
          entry_revision_number: 1,
          files: [
            {
              id: "f1",
              roles: ["primary"],
              name: "model.cif",
              level: "L2",
              type: "model",
              format: "mmcif",
              uri: "https://files.example/model.cif",
              size_bytes: 10,
              recorded_sha256: null,
              recorded_sha256_source: "not_recorded",
              recorded_after_save: false,
              checksum: { status: "failed", sha256: null, size_bytes: null, stored_uri: null, copy_status: null, checked_at: "2026-11-15T09:30:00Z" },
              reuse_terms: "cc0",
              source_size_bytes: null,
            },
          ],
        },
      },
    ],
    meta: { next_cursor: "c2" },
  };
  await withBackend(
    (request) =>
      request.url.includes(`/v1/selections/${selectionId}/models`) ? jsonResponse(page) : errorResponse(404, "SELECTIONS_OFF"),
    async (requests) => {
      // when
      const found = await getSelectionFiles(new Request("http://site/x?cursor=c1"), {
        params: Promise.resolve({ selectionId }),
      });
      const off = await getSelectionFiles(new Request("http://site/x"), {
        params: Promise.resolve({ selectionId: "1b6e4a52-8f0a-4b8e-9d1c-2f6a3e4b5c6d" }),
      });
      const notAnId = await getSelectionFiles(new Request("http://site/x"), { params: Promise.resolve({ selectionId: "../x" }) });

      // then
      assert.equal(found.status, 200);
      assert.deepEqual(await found.json(), {
        models: [
          {
            modelId: "m1",
            entryId: "e1",
            files: [
              {
                fileId: "f1",
                name: "model.cif",
                check: "Checked 2026-11-15 09:30 UTC: could not be read",
                notVerifiedAtSave: true,
                terms: "CC0 1.0",
              },
            ],
          },
        ],
        nextCursor: "c2",
      });
      assert.match(requests[0].url, /\/v1\/selections\/[0-9a-f-]+\/models\?limit=10&cursor=c1$/);
      assert.equal(off.status, 404);
      assert.equal((await off.json()).problem.code, "off");
      assert.equal(notAnId.status, 404);
      assert.equal(requests.length, 2, "an ID that is not a UUID never reaches the backend");
    },
  );
});
