require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { listSimilarEntries } = require("../src/lib/api/entries.ts");
const {
  bestMatchStats,
  deflineTail,
  formatPercent,
  hasRecordedAlignment,
  sequenceLabel,
  similarityStats,
  sortedByScore,
} = require("../src/lib/similarity.ts");

test("should list similar entries with pagination and authorization", async () => {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://backend.example/";
    let requestedURL = "";
    let requestedHeaders = {};
    global.fetch = async (url, init) => {
      requestedURL = String(url);
      requestedHeaders = init.headers;
      return jsonResponse(collectionDocument("similar_entries", [{ entry: { id: "e2" }, score: 0.9, matches: [] }]));
    };

    const items = await listSimilarEntries("token-123", "entry-1", {
      limit: 100,
      offset: 5,
    });

    assert.equal(
      requestedURL,
      "https://backend.example/v1/entries/entry-1/similar-entries?limit=100&offset=5",
    );
    assert.equal(requestedHeaders.Authorization, "Bearer token-123");
    assert.deepEqual(items, [{ entry: { id: "e2" }, score: 0.9, matches: [] }]);
  } finally {
    global.fetch = previousFetch;
    process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
  }
});

test("should read similarity stats defensively from free-form metadata", () => {
  const stats = similarityStats({
    fident: 0.28,
    qcov: 0.84,
    evalue: 1e-28,
    bits: 142.7,
    source_start: 24,
    source_end: 271,
    similar_start: 9,
    similar_end: 244,
    source_alignment: "VLS-",
    similar_alignment: "VLSA",
    qstart: "not a number",
  });
  assert.equal(stats.fident, 0.28);
  assert.equal(stats.qcov, 0.84);
  assert.equal(stats.tcov, null);
  assert.equal(stats.qstart, 24);
  assert.equal(stats.tend, 244);
  assert.equal(stats.qaln, "VLS-");
  assert.equal(stats.taln, "VLSA");

  // mmseqs-native spellings still work as a fallback
  const native = similarityStats({ qstart: 5, qaln: "AA" });
  assert.equal(native.qstart, 5);
  assert.equal(native.qaln, "AA");

  const empty = similarityStats(undefined);
  assert.equal(empty.fident, null);
  assert.equal(empty.qaln, null);
});

test("should accept only alignments that reproduce the real sequences", () => {
  const source = "MKTVLSPADKTNV";
  const similar = "GGVLSAADKTNVPP";
  const exact = similarityStats({
    source_start: 4,
    source_end: 13,
    similar_start: 3,
    similar_end: 12,
    source_alignment: "VLSPADKTNV",
    similar_alignment: "VLSAADKTNV",
  });
  assert.equal(hasRecordedAlignment(exact, source, similar), true);

  const drifted = similarityStats({
    source_start: 3,
    source_end: 13,
    similar_start: 3,
    similar_end: 12,
    source_alignment: "VLSPADKTNV-",
    similar_alignment: "VLSAADKTNVP",
  });
  assert.equal(hasRecordedAlignment(drifted, source, similar), false);

  const placeholder = similarityStats({
    source_alignment: "MMSEQS_DEMO_ALIGNMENT_SOURCE_5GY3_7TIM",
    similar_alignment: "MMSEQS_DEMO_ALIGNMENT_TARGET_7TIM",
  });
  assert.equal(hasRecordedAlignment(placeholder, source, similar), false);

  const absent = similarityStats({ fident: 0.28 });
  assert.equal(hasRecordedAlignment(absent, source, similar), false);
});

test("should pick the strongest numbers across matches", () => {
  const stats = bestMatchStats([
    { metadata: { fident: 0.7, qcov: 0.9 } },
    { metadata: { fident: 0.9, qcov: 0.8 } },
    { metadata: {} },
  ]);
  assert.equal(stats.fident, 0.9);
  assert.equal(stats.qcov, 0.9);
});

test("should shorten sequence headers into row labels", () => {
  assert.equal(
    sequenceLabel("2MHB_1|Chain A|Hemoglobin subunit alpha|Equus caballus", 0),
    "2MHB:A",
  );
  assert.equal(
    sequenceLabel(">4HHB_2|Chains B,D|Hemoglobin subunit beta|Homo sapiens", 1),
    "4HHB:B,D",
  );
  assert.equal(sequenceLabel("P69905 HBA_HUMAN Hemoglobin", 0), "P69905");
  assert.equal(sequenceLabel("", 2), "seq 3");
});

test("should keep the human-readable defline fields for the band", () => {
  assert.deepEqual(
    deflineTail("2MHB_1|Chain A|Hemoglobin subunit alpha|Equus caballus"),
    ["Hemoglobin subunit alpha", "Equus caballus"],
  );
  assert.deepEqual(deflineTail("P69905|HBA_HUMAN"), ["HBA_HUMAN"]);
});

test("should format percentages for the table", () => {
  assert.equal(formatPercent(0.865), "86.5%");
  assert.equal(formatPercent(1), "100%");
  assert.equal(formatPercent(0.9999), "100%");
  assert.equal(formatPercent(null), "—");
});

test("should keep the best hits first", () => {
  const sorted = sortedByScore([
    { entry: { id: "a" }, score: 0.2, matches: [] },
    { entry: { id: "b" }, score: 0.9, matches: [] },
  ]);
  assert.deepEqual(
    sorted.map((item) => item.entry.id),
    ["b", "a"],
  );
});

function jsonResponse(body) {
  return {
    ok: true,
    status: 200,
    json: async () => body,
  };
}

function collectionDocument(type, items) {
  return {
    data: items.map((attributes) => ({
      type,
      id: attributes.entry.id,
      attributes,
    })),
  };
}
