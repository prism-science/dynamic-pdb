require("./register.cjs");

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const React = require("react");
const { renderToStaticMarkup } = require("react-dom/server");

const {
  ARCHIVE_DAYS,
  ARCHIVE_REQUESTS_PER_MINUTE,
  downloadLimits,
  MAX_ARCHIVE_BYTES,
  PRODUCTION_API_BASE_URL,
  SCRIPT_PATH,
  scriptExamples,
  scriptPageHref,
  selectionFromQuery,
  siteOriginFor,
} = require("../src/lib/download-instructions.ts");
const { REUSE_TERMS } = require("../src/lib/download-selection.ts");
const { resetBulkDownloadAvailability } = require("../src/lib/api/downloads.ts");
const Download = require("../src/app/download/page.tsx").default;

const repoRoot = path.join(__dirname, "..", "..");
const selectionId = "3f0c2a5e-8d1b-4c7a-9e2f-1a2b3c4d5e6f";

test("should read the selection ID from ?selection= and nothing else", () => {
  assert.equal(selectionFromQuery(selectionId), selectionId);
  assert.equal(selectionFromQuery(` ${selectionId.toUpperCase()} `), selectionId);
  assert.equal(selectionFromQuery([selectionId, "other"]), selectionId);
  assert.equal(selectionFromQuery(undefined), null);
  assert.equal(selectionFromQuery(""), null);
  assert.equal(selectionFromQuery("3f0c2a5e"), null);
  assert.equal(selectionFromQuery(`${selectionId}' && rm -rf ~`), null, "nothing but an ID reaches a command");
});

test("should link the script section for the selection, or without one", () => {
  assert.equal(scriptPageHref(selectionId), `/download?selection=${selectionId}#script`);
  assert.equal(scriptPageHref(null), "/download#script");
});

test("should find the website's address only when the API is served under it at /api", () => {
  assert.equal(siteOriginFor("https://dynamicpdb.com/api"), "https://dynamicpdb.com");
  assert.equal(siteOriginFor("https://dev.dynamicpdb.com/api/"), "https://dev.dynamicpdb.com");
  assert.equal(siteOriginFor("http://127.0.0.1:19290"), null);
  assert.equal(siteOriginFor("not a url"), null);
});

test("should write the examples out for the selection on the production API", () => {
  const examples = scriptExamples({ selectionId, apiBaseUrl: PRODUCTION_API_BASE_URL });

  assert.equal(
    examples.script,
    [
      "curl -O https://dynamicpdb.com/download/dynamic-pdb-download.py",
      "python3 dynamic-pdb-download.py \\",
      `  --selection ${selectionId} \\`,
      "  --files all_files",
    ].join("\n"),
  );
  assert.equal(
    examples.archive,
    [
      "# 1. Ask for the archive; it is built right away",
      "curl -s -X POST 'https://dynamicpdb.com/api/v1/downloads' \\",
      "  -H 'Content-Type: application/vnd.api+json' \\",
      `  -d '{"selection_id": "${selectionId}",`,
      `       "file_choice": "all_files", "prepare": true}'`,
      "# 2. Ask again until data.attributes.status is ready",
      "#    (blocked means over the limit: use the script)",
      "curl -s 'https://dynamicpdb.com/api/v1/downloads/<archive id>'",
      "# 3. Download data.attributes.archive_url, then check it:",
      "#    shasum must print data.attributes.archive_sha256",
      `curl -o dynamic-pdb-${selectionId}-all_files.zip '<archive_url>'`,
      `shasum -a 256 dynamic-pdb-${selectionId}-all_files.zip`,
    ].join("\n"),
  );
  assert.match(examples.saveSelection, /^curl -s -X POST 'https:\/\/dynamicpdb\.com\/api\/v1\/selections'/);
  // The request body is valid JSON once the placeholders are filled.
  const body = /-d '(\{[\s\S]*?\})'/.exec(examples.archive)[1];
  assert.deepEqual(JSON.parse(body), { selection_id: selectionId, file_choice: "all_files", prepare: true });
});

test("should show a placeholder without a selection, and name another API in the script's command", () => {
  const examples = scriptExamples({ selectionId: null, apiBaseUrl: "http://127.0.0.1:19290/", fileChoice: "original_mmcif" });

  assert.equal(
    examples.script,
    "python3 dynamic-pdb-download.py \\\n  --selection <selection id> \\\n  --files original_mmcif \\\n  --api http://127.0.0.1:19290",
    "no download line when the website's address is unknown; --api for a server other than production",
  );
  assert.match(examples.archive, /"selection_id": "<selection id>",\n {7}"file_choice": "original_mmcif"/);
  assert.match(examples.archive, /curl -s 'http:\/\/127\.0\.0\.1:19290\/v1\/downloads\/<archive id>'/);
});

test("should state the limits the backend applies by default, unchanged in production", () => {
  const config = fs.readFileSync(path.join(repoRoot, "backend", "internal", "config", "config.go"), "utf8");
  const production = fs.readFileSync(path.join(repoRoot, "backend", "config", "production.yml"), "utf8");
  const constant = (name) => {
    const match = new RegExp(`${name}\\s*=\\s*(.+)`).exec(config);
    assert.ok(match, name);
    return match[1].trim();
  };

  assert.equal(constant("defaultDownloadsMaxArchiveBytes"), `int64(${MAX_ARCHIVE_BYTES.toLocaleString("en-US").replaceAll(",", "_")})`);
  assert.equal(constant("defaultDownloadsArchiveTTL"), `${ARCHIVE_DAYS} * 24 * time.Hour`);
  assert.equal(constant("defaultDownloadsRateLimitRequests"), String(ARCHIVE_REQUESTS_PER_MINUTE));
  assert.equal(constant("defaultDownloadsRateLimitWindow"), "time.Minute");
  const downloadsBlock = /\ndownloads:\n((?:[ #].*\n)*)/.exec(production)[1];
  assert.doesNotMatch(downloadsBlock, /^\s+(max_archive_bytes|archive_ttl|rate_limit):/m, "production keeps the defaults");

  const limits = downloadLimits();
  assert.equal(limits.length, 5);
  assert.match(limits[0], /^One archive holds at most 10 GB \(10,000,000,000 bytes, before compression\)\./);
  assert.match(limits[1], /over 1 GiB are linked, not copied/);
  assert.match(limits[2], /kept for 7 days/);
  assert.match(limits[4], /10 archives a minute/);
});

test("should name the production API the script and the website use by default", () => {
  const script = fs.readFileSync(path.join(__dirname, "..", "public", "download", "dynamic-pdb-download.py"), "utf8");
  assert.match(script, new RegExp(`^DEFAULT_API = "${PRODUCTION_API_BASE_URL}"$`, "m"));
  const baseUrl = fs.readFileSync(path.join(__dirname, "..", "src", "lib", "api", "baseUrl.ts"), "utf8");
  assert.ok(baseUrl.includes(`"${PRODUCTION_API_BASE_URL}"`));
  assert.ok(fs.existsSync(path.join(__dirname, "..", "public", ...SCRIPT_PATH.split("/").filter(Boolean))));
});

/** Renders the page with the backend's switches answered as on or off. */
async function renderPage({ on, query = {} }) {
  const previousFetch = global.fetch;
  const previousApiBaseURL = process.env.NEXT_PUBLIC_API_BASE_URL;
  resetBulkDownloadAvailability();
  try {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://dynamicpdb.com/api";
    global.fetch = async (url) => {
      const off = String(url).includes("/v1/selections/") ? "SELECTIONS_OFF" : "DOWNLOADS_OFF";
      const code = on ? "NOT_FOUND" : off;
      return new Response(JSON.stringify({ errors: [{ status: "404", code }] }), {
        status: 404,
        headers: { "content-type": "application/vnd.api+json" },
      });
    };
    const element = await Download({ searchParams: Promise.resolve(query) });
    return renderToStaticMarkup(React.createElement(React.Fragment, null, element));
  } finally {
    global.fetch = previousFetch;
    if (previousApiBaseURL === undefined) {
      delete process.env.NEXT_PUBLIC_API_BASE_URL;
    } else {
      process.env.NEXT_PUBLIC_API_BASE_URL = previousApiBaseURL;
    }
    resetBulkDownloadAvailability();
  }
}

test("should keep today's text while the server has bulk download off", async () => {
  const html = await renderPage({ on: false, query: { selection: selectionId } });

  assert.match(html, /Bulk download is not available yet\./);
  assert.match(html, /Individual files can be downloaded from any entry page\./);
  assert.doesNotMatch(html, /id="script"/);
  assert.doesNotMatch(html, new RegExp(selectionId));
});

test("should show the instructions, the limits, the CC0 text exactly and the script for the selection", async () => {
  const html = await renderPage({ on: true, query: { selection: selectionId } });

  assert.doesNotMatch(html, /not available yet/);
  assert.ok(html.includes(` data-reuse-terms="">${REUSE_TERMS}</p>`), "the CC0 text, word for word");
  assert.match(html, /href="https:\/\/creativecommons\.org\/publicdomain\/zero\/1\.0\/"/);
  assert.match(html, /<section[^>]* id="script"/);
  assert.match(html, new RegExp(`data-script-selection="${selectionId}"`));
  assert.match(html, new RegExp(`--selection ${selectionId} \\\\\n  --files all_files`));
  assert.match(html, /href="\/download\/dynamic-pdb-download\.py" download="dynamic-pdb-download\.py"/);
  assert.match(html, /on macOS or Linux\. On Windows, run it in WSL \(Windows Subsystem for Linux\), or use the archive\./);
  for (const line of downloadLimits()) {
    assert.ok(html.includes(line.replaceAll("'", "&#x27;")), line);
  }
});

test("should show placeholders when ?selection= is missing or not an ID", async () => {
  const html = await renderPage({ on: true, query: { selection: "<script>" } });

  assert.match(html, /data-script-selection=""/);
  assert.match(html, /--selection &lt;selection id&gt; \\\n {2}--files all_files/);
  assert.doesNotMatch(html, /<script>/);
});
