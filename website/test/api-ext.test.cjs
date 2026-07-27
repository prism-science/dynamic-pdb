require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  extFileReferenceURL,
  extFileKey,
  getExtExperiment,
  parseExtExperimentRef,
  parseExtFileReference,
  resolveFileURL,
  searchExtFiles,
} = require("../src/lib/api/ext.ts");

test("should search Ext files with query and cursor", async () => {
  const previousFetch = global.fetch;
  try {
    let request = null;
    global.fetch = async (url, init) => {
      request = { url: String(url), init };
      return jsonResponse({
        items: [{
          path: "/data/model.cif",
          sha256: "abc",
          size: 42,
          url: "https://files.example/model.cif?signature=one",
        }],
        refs: [{
          direction: "output",
          file_ref: { path: "/data/model.cif", sha256: "abc" },
        }],
        next_cursor: "next",
      });
    };

    const page = await searchExtFiles("experiment-id", {
      query: "  model  ",
      cursor: "cursor-1",
    });

    assert.equal(
      request.url,
      "https://extshell.org/api/v1/experiments/experiment-id/files?q=model&cursor=cursor-1&limit=50",
    );
    assert.equal(request.init.cache, "no-store");
    assert.equal(page.next_cursor, "next");
    assert.equal(page.items[0].available, true);
    assert.deepEqual(page.items[0].directions, ["output"]);
  } finally {
    global.fetch = previousFetch;
  }
});

test("should parse an experiment id out of whatever the user pastes", () => {
  const experimentId = "6d95a158-f380-4185-8d6c-f52a63145667";
  for (const input of [
    experimentId,
    `  ${experimentId.toUpperCase()}  `,
    `https://extshell.org/experiments/${experimentId}`,
    `https://extshell.org/experiments/${experimentId}/`,
    `https://extshell.org/experiments/${experimentId}?tab=files#top`,
    `extshell.org/experiments/${experimentId}`,
    `http://localhost:3000/experiments/${experimentId}`,
    `<https://extshell.org/experiments/${experimentId}>`,
    `"https://extshell.org/experiments/${experimentId}".`,
  ]) {
    assert.equal(parseExtExperimentRef(input), experimentId, input);
  }

  for (const input of [
    "",
    "   ",
    "6d95a158",
    "https://extshell.org/experiments/not-a-uuid",
    "https://extshell.org/experiments",
    // A UUID at the end of some unrelated URL is not an experiment link.
    `https://drive.example.com/files/${experimentId}`,
    `javascript:alert(1)/experiments/${experimentId}`,
    `ext://${experimentId}?path=/a.cif`,
  ]) {
    assert.equal(parseExtExperimentRef(input), null, input);
  }
});

test("should build and parse a stable frontend Ext file reference", () => {
  const experimentId = "123e4567-e89b-42d3-a456-426614174000";
  const reference = extFileReferenceURL(experimentId, {
    path: "/data/model with spaces.cif",
    sha256: "abc",
  });
  assert.equal(
    reference,
    `ext://${experimentId}?path=%2Fdata%2Fmodel+with+spaces.cif&sha256=abc`,
  );
  assert.deepEqual(
    parseExtFileReference(reference),
    {
      experimentId,
      path: "/data/model with spaces.cif",
      sha256: "abc",
    },
  );
  assert.equal(
    extFileKey({ path: "/data/model.cif", sha256: "abc" }),
    "/data/model.cif\u0000abc",
  );
});

test("should resolve a stored Ext reference to a fresh file URL", async () => {
  const previousFetch = global.fetch;
  try {
    const experimentId = "123e4567-e89b-42d3-a456-426614174001";
    global.fetch = async () =>
      jsonResponse({
        items: [{
          path: "/data/model.cif",
          sha256: "sha-1",
          size: 42,
          url: "https://files.example/model.cif?signature=fresh",
        }],
      });

    const resolved = await resolveFileURL(
      extFileReferenceURL(experimentId, {
        path: "/data/model.cif",
        sha256: "sha-1",
      }),
    );

    assert.equal(
      resolved,
      "https://files.example/model.cif?signature=fresh",
    );
  } finally {
    global.fetch = previousFetch;
  }
});

test("should load only public experiments directly from Ext", async () => {
  const previousFetch = global.fetch;
  try {
    const experimentId = "123e4567-e89b-42d3-a456-426614174002";
    global.fetch = async () =>
      jsonResponse({
        id: experimentId,
        name: "Lysozyme",
        description: "Public data",
        visibility: "public",
      });

    const experiment = await getExtExperiment(experimentId);

    assert.equal(experiment.name, "Lysozyme");
    assert.equal(
      experiment.web_url,
      `https://extshell.org/experiments/${experimentId}`,
    );
  } finally {
    global.fetch = previousFetch;
  }
});

function jsonResponse(body, init = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  });
}
