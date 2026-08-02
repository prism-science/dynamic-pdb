require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { uploadFileToObjectStorage } = require("../src/lib/api/uploads.ts");

test("should upload all granted parts and complete upload", async () => {
  const restore = installUploadFakes({
    grant: {
      key: "structure/entities/entity/data.bin",
      upload_id: "upload-id",
      object_url: "https://dynamic-pdb-data.s3.us-west-1.amazonaws.com/structure/entities/entity/data.bin",
      part_size: 3,
      parts: [
        { part_number: 1, url: "https://storage.example/part-1" },
        { part_number: 2, url: "https://storage.example/part-2" },
      ],
    },
    xhrResults: [
      { status: 200, headers: { etag: '"etag-1"' }, loaded: 3 },
      { status: 200, headers: { etag: '"etag-2"' }, loaded: 3 },
    ],
  });
  try {
    const progress = [];

    const objectURL = await uploadFileToObjectStorage(
      new File(["abcdef"], "data.bin"),
      { structureId: "structure", modelId: null, entityId: "entity" },
      (fraction) => progress.push(fraction),
    );

    assert.equal(objectURL, "https://dynamic-pdb-data.s3.us-west-1.amazonaws.com/structure/entities/entity/data.bin");
    assert.deepEqual(FakeXMLHttpRequest.instances.map((xhr) => xhr.url), [
      "https://storage.example/part-1",
      "https://storage.example/part-2",
    ]);
    assert.deepEqual(restore.calls.complete.parts, [
      { part_number: 1, etag: '"etag-1"' },
      { part_number: 2, etag: '"etag-2"' },
    ]);
    assert.equal(progress.at(-1), 1);
  } finally {
    restore();
  }
});

test("should abort upload and keep original error when object storage omits etag", async () => {
  const restore = installUploadFakes({
    grant: {
      key: "structure/entities/entity/data.bin",
      upload_id: "upload-id",
      object_url: "https://dynamic-pdb-data.s3.us-west-1.amazonaws.com/structure/entities/entity/data.bin",
      part_size: 6,
      parts: [{ part_number: 1, url: "https://storage.example/part-1" }],
    },
    xhrResults: [{ status: 200, headers: {} }],
  });
  try {
    await assert.rejects(
      () =>
        uploadFileToObjectStorage(new File(["abcdef"], "data.bin"), {
          structureId: "structure",
          modelId: null,
          entityId: "entity",
        }),
      /Object storage did not return ETag/,
    );
    assert.deepEqual(restore.calls.abort, {
      key: "structure/entities/entity/data.bin",
      upload_id: "upload-id",
    });
  } finally {
    restore();
  }
});

test("should abort upload when complete endpoint fails", async () => {
  const restore = installUploadFakes({
    grant: {
      key: "structure/entities/entity/data.bin",
      upload_id: "upload-id",
      object_url: "https://dynamic-pdb-data.s3.us-west-1.amazonaws.com/structure/entities/entity/data.bin",
      part_size: 6,
      parts: [{ part_number: 1, url: "https://storage.example/part-1" }],
    },
    completeStatus: 500,
    completeBody: { error: "complete failed" },
    xhrResults: [{ status: 200, headers: { etag: '"etag-1"' } }],
  });
  try {
    await assert.rejects(
      () =>
        uploadFileToObjectStorage(new File(["abcdef"], "data.bin"), {
          structureId: "structure",
          modelId: null,
          entityId: "entity",
        }),
      /complete failed/,
    );
    assert.deepEqual(restore.calls.abort, {
      key: "structure/entities/entity/data.bin",
      upload_id: "upload-id",
    });
  } finally {
    restore();
  }
});

test("should reject invalid upload grant without aborting", async () => {
  const restore = installUploadFakes({
    grant: {
      key: "",
      upload_id: "upload-id",
      object_url: "https://dynamic-pdb-data.s3.us-west-1.amazonaws.com/structure/entities/entity/data.bin",
      part_size: 0,
      parts: [],
    },
  });
  try {
    await assert.rejects(
      () =>
        uploadFileToObjectStorage(new File(["abcdef"], "data.bin"), {
          structureId: "structure",
          modelId: null,
          entityId: "entity",
        }),
      /invalid file upload grant/,
    );
    assert.equal(restore.calls.abort, null);
  } finally {
    restore();
  }
});

function installUploadFakes({
  grant,
  completeStatus = 200,
  completeBody = {},
  xhrResults = [],
}) {
  const previousFetch = global.fetch;
  const previousXHR = global.XMLHttpRequest;
  const calls = { create: null, complete: null, abort: null };
  FakeXMLHttpRequest.queue = [...xhrResults];
  FakeXMLHttpRequest.instances = [];
  global.XMLHttpRequest = FakeXMLHttpRequest;
  global.fetch = async (path, init) => {
    if (path === "/files") {
      calls.create = JSON.parse(init.body);
      return jsonResponse(grant);
    }
    if (path === "/files/complete") {
      calls.complete = JSON.parse(init.body);
      return jsonResponse(completeBody, { status: completeStatus });
    }
    if (path === "/files/abort") {
      calls.abort = JSON.parse(init.body);
      return new Response(null, { status: 204 });
    }
    throw new Error(`unexpected fetch ${path}`);
  };

  function restore() {
    global.fetch = previousFetch;
    global.XMLHttpRequest = previousXHR;
  }
  restore.calls = calls;
  return restore;
}

class FakeXMLHttpRequest {
  static queue = [];
  static instances = [];

  constructor() {
    this.upload = {};
    this.status = 0;
    this.responseText = "";
    this.responseHeaders = {};
    FakeXMLHttpRequest.instances.push(this);
  }

  open(method, url) {
    this.method = method;
    this.url = url;
  }

  getResponseHeader(name) {
    return this.responseHeaders[name.toLowerCase()] ?? null;
  }

  send(body) {
    this.body = body;
    const result = FakeXMLHttpRequest.queue.shift() ?? { status: 200, headers: { etag: '"etag"' } };
    if (result.loaded != null) {
      this.upload.onprogress?.({ lengthComputable: true, loaded: result.loaded });
    }
    if (result.error) {
      this.onerror?.();
      return;
    }
    this.status = result.status ?? 200;
    this.responseText = result.responseText ?? "";
    this.responseHeaders = normalizeHeaders(result.headers ?? {});
    this.onload?.();
  }
}

function normalizeHeaders(headers) {
  return Object.fromEntries(
    Object.entries(headers).map(([key, value]) => [key.toLowerCase(), value]),
  );
}

function jsonResponse(body, init = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  });
}
