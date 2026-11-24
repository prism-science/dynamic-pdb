import { execFile, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import http from "node:http";
import type { AddressInfo } from "node:net";
import path from "node:path";

import type { Page } from "@playwright/test";

import { createActiveEntry, createActiveModel, createActiveModelWithFile } from "./support/api";
import { markStoredFileVerified } from "./support/database";
import { freshPdbId } from "./support/fakeRcsb";
import { expect, test } from "./support/fixtures";
import { archiveWorkerEnvironment, stack } from "./support/stack";

// The bulk-download panel (B3, L2 UI) against the real backend, with
// selections and downloads on (backend/config/local.yml), and the archive
// worker run by the test whenever an archive waits: once to add the files up
// for the preview, once more to build it. Each test makes its own models with
// PDB IDs no other record has, so its searches see only them.

const backendDirectory = path.join(__dirname, "../../backend");

/**
 * Plans and builds the waiting archives, then exits (archive_worker.exit_when_idle).
 * Asynchronous, so a stand-in server in this process keeps answering meanwhile.
 * Its log is kept out of the test output; a failed run rejects with it.
 */
function runArchiveWorker(overrides: Record<string, string> = {}): Promise<void> {
  return new Promise((resolve, reject) => {
    execFile(
      path.join(backendDirectory, "bin/archive-worker"),
      [],
      { cwd: backendDirectory, env: { ...process.env, ...archiveWorkerEnvironment(), ...overrides }, timeout: 120_000 },
      (error, _stdout, stderr) => (error ? reject(new Error(`archive worker: ${error.message}\n${stderr}`)) : resolve()),
    );
  });
}

/**
 * An S3 endpoint in front of the S3 stand-in that answers 500 to reads of the
 * keys containing failingPath and passes everything else on: a copy that
 * cannot be read while the archive is built (read_failed).
 */
async function startFailingS3(failingPath: string): Promise<{ url: string; close: () => Promise<void> }> {
  const target = new URL(stack.s3URL);
  const server = http.createServer((request, response) => {
    if ((request.method === "GET" || request.method === "HEAD") && request.url?.includes(failingPath)) {
      response.writeHead(500, { "Content-Type": "application/xml" });
      response.end("<Error><Code>InternalError</Code><Message>injected by the browser test</Message></Error>");
      return;
    }
    const upstream = http.request(
      { host: target.hostname, port: target.port, method: request.method, path: request.url, headers: request.headers },
      (answer) => {
        response.writeHead(answer.statusCode ?? 502, answer.headers);
        answer.pipe(response);
      },
    );
    upstream.on("error", () => {
      response.writeHead(502);
      response.end();
    });
    request.pipe(upstream);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${port}`,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

/** The drawer's archive card for the first archive, or a retry. */
function archiveCards(page: Page) {
  return page.getByRole("dialog", { name: "Download selection" }).locator("[data-archive-id]");
}

/** Stores a file's bytes in the S3 stand-in, where an upload would have put them. */
async function storeFile(uri: string, content: string): Promise<void> {
  const response = await fetch(uri, { method: "PUT", body: content });
  if (!response.ok) {
    throw new Error(`store ${uri}: HTTP ${response.status}`);
  }
}

function unzipList(zipPath: string): string[] {
  return execFileSync("unzip", ["-Z1", zipPath], { encoding: "utf8" }).split("\n").filter(Boolean);
}

function unzipFile(zipPath: string, entry: string): Buffer {
  return execFileSync("unzip", ["-p", zipPath, entry]);
}

const downloadScript = path.join(__dirname, "../public/download/dynamic-pdb-download.py");
const SELECTION_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const REUSE_TERMS =
  "PDB archive files and Prism's own models are dedicated to the public domain under CC0 1.0. Files deposited by others: Reuse terms not yet confirmed by the depositor. We ask that you acknowledge Dynamic PDB and cite the depositors of the entries you use.";

/**
 * Runs the download script (B4) on a saved selection against the test stack, as a reader would
 * against dynamicpdb.com: our storage is the S3 stand-in's bucket. Resolves with the folder it
 * wrote; a failed run rejects with its output.
 */
function runDownloadScript(selectionId: string, fileChoice: string, out: string): Promise<string> {
  const args = [
    "-I",
    "-B",
    downloadScript,
    "--selection",
    selectionId,
    "--files",
    fileChoice,
    "--out",
    out,
    "--api",
    stack.backendURL,
    "--storage-base",
    `${stack.s3URL}/${stack.s3Bucket}`,
  ];
  return new Promise((resolve, reject) => {
    execFile("python3", args, { timeout: 120_000 }, (error, _stdout, stderr) =>
      error ? reject(new Error(`download script: ${error.message}\n${stderr}`)) : resolve(path.join(out, `dynamic-pdb-${selectionId}`)),
    );
  });
}

/** Asks the backend for an archive of a saved selection, built right away, and returns its ID. */
async function requestArchive(selectionId: string, fileChoice: string): Promise<string> {
  const response = await fetch(`${stack.backendURL}/v1/downloads`, {
    method: "POST",
    headers: { "Content-Type": "application/vnd.api+json" },
    body: JSON.stringify({ selection_id: selectionId, file_choice: fileChoice, prepare: true }),
  });
  const body = (await response.json()) as { data: { id: string } };
  if (response.status !== 202) {
    throw new Error(`request archive: HTTP ${response.status}: ${JSON.stringify(body)}`);
  }
  return body.data.id;
}

/** A ready archive's manifest.tsv, read from where the API says it is. */
async function readyManifest(archiveId: string): Promise<string> {
  const response = await fetch(`${stack.backendURL}/v1/downloads/${archiveId}`);
  const body = (await response.json()) as { data: { attributes: { status: string; manifest_url?: string } } };
  const { status, manifest_url: manifestURL } = body.data.attributes;
  if (status !== "ready" || manifestURL === undefined) {
    throw new Error(`archive ${archiveId}: status ${status}`);
  }
  return (await fetch(manifestURL)).text();
}

/**
 * The manifest columns the script and the archive must agree on: which records and files, their
 * revisions, paths, statuses, checksums and sources. verified_at differs by design (each says when
 * it read the bytes), and the script leaves the citation columns to the archive.
 */
const COMPARED_COLUMNS = [
  "record_type", "entry_id", "entry_revision", "model_id", "model_revision", "model_type", "file_id",
  "file_name", "path_in_archive", "roles", "type", "level", "format", "size_bytes", "size_source",
  "recorded_sha256", "sha256_source", "recorded_after_save", "verified_at_save", "status", "reason", "reason_code",
  "verification", "source", "source_url", "license", "license_url",
];

/** The rows both list (files and models without a file of the choice), in their columns. */
function comparableRows(rows: Record<string, string>[]): string[][] {
  return rows
    .filter((row) => row.record_type !== "related_dataset")
    .map((row) => COMPARED_COLUMNS.map((column) => row[column] ?? ""));
}

/** manifest.tsv as rows keyed by column name; the # lines above the header are terms. */
function manifestRows(text: string): Record<string, string>[] {
  const lines = text.split("\n").filter((line) => line !== "" && !line.startsWith("#"));
  const [header, ...rows] = lines;
  const columns = header.split("\t");
  return rows.map((row) => {
    const values = row.split("\t");
    return Object.fromEntries(columns.map((column, index) => [column, values[index] ?? ""]));
  });
}

test("should select models, review them and download one archive of the selection", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}, testInfo) => {
  // given entry A with a Deposited and a Multiconformer model, whose file we hold. Each model
  // carries its entry's PDB ID: a model whose own PDB ID differs from its entry's is not
  // approved until the grouping rules pass (researchers' Q11, patch 32).
  const [pdbA, pdbB] = [freshPdbId(), freshPdbId()];
  const entryA = await createActiveEntry(submitterApi, reviewerApi, `B3 bulk A ${runTag}`, {
    external_refs: { pdb: pdbA },
  });
  const entryB = await createActiveEntry(submitterApi, reviewerApi, `B3 bulk B ${runTag}`, {
    external_refs: { pdb: pdbB },
  });
  const depositedA = await createActiveModel(submitterApi, reviewerApi, entryA, {
    title: `B3 deposited A ${runTag}`,
    modelType: "Deposited",
    rFree: 0.2,
    pdbId: pdbA,
  });
  const modelFileContent = `REMARK bulk download browser test ${runTag}\nEND\n`;
  const multiconformerA = await createActiveModelWithFile(submitterApi, reviewerApi, entryA, {
    title: `B3 multiconformer A ${runTag}`,
    modelType: "Multiconformer",
    rFree: 0.15,
    pdbId: pdbA,
    modelFileContent,
  });
  await storeFile(multiconformerA.modelFile.uri, modelFileContent);
  markStoredFileVerified(
    multiconformerA.modelFile.artifactId,
    multiconformerA.modelFile.sha256,
    multiconformerA.modelFile.sizeBytes,
  );
  // and entry B with a Deposited, an Ensemble and a Single Conformer model
  const depositedB = await createActiveModel(submitterApi, reviewerApi, entryB, {
    title: `B3 deposited B ${runTag}`,
    modelType: "Deposited",
    rFree: 0.21,
    pdbId: pdbB,
  });
  const ensembleB = await createActiveModel(submitterApi, reviewerApi, entryB, {
    title: `B3 ensemble B ${runTag}`,
    modelType: "Ensemble",
    rFree: 0.18,
    pdbId: pdbB,
    structureFactorsWithoutSha256: true,
  });
  const singleConformerB = await createActiveModel(submitterApi, reviewerApi, entryB, {
    title: `B3 single conformer B ${runTag}`,
    modelType: "Single Conformer",
    rFree: 0.19,
    pdbId: pdbB,
  });

  // when the reader selects all results of a search and leaves one out
  await page.goto(`/browse?pdb_id=${pdbB}&model_type=Ensemble&model_type=${encodeURIComponent("Single Conformer")}`);
  await page.getByRole("button", { name: "Select all 2 results" }).click();
  await expect(page.getByText("All 2 results are selected.")).toBeVisible();
  const singleConformerBox = page.getByRole("checkbox", { name: new RegExp(`Select B3 single conformer B ${runTag}`) });
  await expect(singleConformerBox).toBeChecked();
  await singleConformerBox.uncheck();
  await expect(page.getByRole("checkbox", { name: new RegExp(`Select B3 ensemble B ${runTag}`) })).toBeChecked();

  // and picks a model in another search, which the bar counts across searches
  await page.goto(`/browse?pdb_id=${pdbA}&model_type=Multiconformer`);
  await page.getByRole("checkbox", { name: new RegExp(`Select B3 multiconformer A ${runTag}`) }).check();
  await expect(page.getByRole("region", { name: "Download selection" })).toContainText("About 2 models selected");

  // and finds it ticked on its entry page, next to the labelled Deposited model
  await page.goto(`/entries/${entryA}`);
  await page.getByRole("button", { name: /Select for download/ }).click();
  await expect(page.getByRole("checkbox", { name: `B3 multiconformer A ${runTag}` })).toBeChecked();
  await expect(
    page.getByRole("checkbox", { name: `B3 deposited A ${runTag} (Deposited: holds the PDB's original mmCIF)` }),
  ).not.toBeChecked();

  // and reviews the selection, adds the entries' Deposited models and starts the download
  await page.getByRole("button", { name: "Review and download" }).click();
  const drawer = page.getByRole("dialog", { name: "Download selection" });
  await expect(drawer.getByText(new RegExp(`PDB ID: ${pdbB}`))).toBeVisible();
  await expect(drawer.locator(`[data-review-model="${multiconformerA.modelId}"]`)).toBeVisible();
  await expect(drawer.locator(`[data-review-model="${singleConformerB}"]`)).toContainText("Take back");
  await drawer.getByRole("checkbox", { name: "Also select each entry’s Deposited model" }).check();
  await drawer.getByRole("radio", { name: /All available files/ }).check();
  await drawer.getByRole("button", { name: "Check the archive" }).click();

  // then the selection is saved with both entries' Deposited models, minus the left-out model
  await expect(drawer.getByText("Saved: 4 models in 2 entries.")).toBeVisible();
  await expect(drawer.getByRole("heading", { name: "Archive: All available files" })).toBeVisible();

  // and once the worker has added the files up, the preview shows what the archive will hold
  await runArchiveWorker();
  const preview = drawer.locator("[data-preview]");
  await expect(preview).toContainText("4 models in 2 entries", { timeout: 30_000 });
  const previewFiles = Number(/· ([\d,]+) files?/.exec((await preview.textContent()) ?? "")?.[1]?.replace(/,/g, ""));
  // with the files by reuse terms, and no estimated size (researchers' Q7, Q9)
  await expect(preview.locator("[data-preview-terms]")).toContainText(/Reuse terms: \d+ files? CC0 1\.0 · \d+ files?: reuse terms not yet confirmed/);
  await expect(preview).not.toContainText(/about|upper bound/);

  // and once the reader builds it and the worker has, the archive downloads
  await drawer.getByRole("button", { name: "Build the archive" }).click();
  await runArchiveWorker();
  const downloadLink = drawer.getByRole("link", { name: /Download zip/ });
  await expect(downloadLink).toBeVisible({ timeout: 30_000 });
  await expect(drawer.locator("[data-reuse-terms]")).toContainText(REUSE_TERMS);
  const [download] = await Promise.all([page.waitForEvent("download"), downloadLink.click()]);
  const zipPath = testInfo.outputPath("archive.zip");
  await download.saveAs(zipPath);

  // and its manifest names exactly the selected models, and the held file's bytes match their SHA-256
  const entries = unzipList(zipPath);
  const manifestEntry = entries.find((entry) => entry.endsWith("/manifest.tsv"));
  expect(manifestEntry, entries.join("\n")).toBeDefined();
  const rows = manifestRows(unzipFile(zipPath, manifestEntry as string).toString("utf8"));
  const fileRows = rows.filter((row) => row.record_type === "file");
  expect(new Set(fileRows.map((row) => row.model_id))).toEqual(
    new Set([depositedA, multiconformerA.modelId, depositedB, ensembleB]),
  );
  expect(new Set(fileRows.map((row) => row.file_id)).size, "the preview counts the archive's files").toBe(previewFiles);
  const heldFile = fileRows.find((row) => row.file_id === multiconformerA.modelFile.artifactId);
  expect(heldFile?.status).toBe("included");
  expect(heldFile?.recorded_sha256).toBe(multiconformerA.modelFile.sha256);
  const heldEntry = entries.find((entry) => entry.endsWith(`/${heldFile?.path_in_archive}`) || entry === heldFile?.path_in_archive);
  expect(heldEntry, entries.join("\n")).toBeDefined();
  const bytes = unzipFile(zipPath, heldEntry as string);
  expect(createHash("sha256").update(bytes).digest("hex")).toBe(multiconformerA.modelFile.sha256);

  // and the reader can see each file's last check, and which are not verified at save time (Q10)
  await drawer.getByRole("button", { name: "Show each file’s last check" }).click();
  const heldCheck = drawer.locator(`[data-file-check="${multiconformerA.modelFile.artifactId}"]`);
  await expect(heldCheck).toContainText(/Checked \d{4}-\d\d-\d\d \d\d:\d\d UTC: the bytes match the record/);
  await expect(heldCheck).toContainText("Reuse terms not yet confirmed by the depositor");
  await expect(heldCheck.locator("[data-not-verified-at-save]")).toHaveCount(0);
  const unrecorded = drawer.locator(`[data-file-checks-model="${ensembleB}"] [data-not-verified-at-save]`);
  await expect(unrecorded).toHaveText(["not verified against the bytes at save time"]);

  // and the panel names the saved selection, for the script (B4)
  const selectionLine = drawer.locator("[data-selection-id]");
  const selectionId = (await selectionLine.getAttribute("data-selection-id")) ?? "";
  expect(selectionId).toMatch(SELECTION_ID);
  await expect(selectionLine).toContainText(`Selection ID ${selectionId}`);

  // when the script downloads the same selection, for each file choice
  const otherArchives = {
    original_mmcif: await requestArchive(selectionId, "original_mmcif"),
    modified_coordinates: await requestArchive(selectionId, "modified_coordinates"),
  };
  await runArchiveWorker();
  const archiveManifests: Record<string, Record<string, string>[]> = {
    all_files: rows,
    original_mmcif: manifestRows(await readyManifest(otherArchives.original_mmcif)),
    modified_coordinates: manifestRows(await readyManifest(otherArchives.modified_coordinates)),
  };
  for (const [fileChoice, archiveRows] of Object.entries(archiveManifests)) {
    const folder = await runDownloadScript(selectionId, fileChoice, testInfo.outputPath(`script-${fileChoice}`));

    // then it lists the same files, revisions, paths, statuses and SHA-256s as the archive
    const scriptRows = manifestRows(readFileSync(path.join(folder, "manifest.tsv"), "utf8"));
    expect(comparableRows(scriptRows), `${fileChoice}: the script's manifest is the archive's`).toEqual(
      comparableRows(archiveRows),
    );
    // and every file it saved has the archive's bytes
    for (const row of scriptRows.filter((candidate) => candidate.status === "included")) {
      const saved = readFileSync(path.join(path.dirname(folder), ...row.path_in_archive.split("/")));
      expect(createHash("sha256").update(saved).digest("hex"), row.path_in_archive).toBe(row.recorded_sha256);
      if (fileChoice === "all_files") {
        expect(saved.equals(unzipFile(zipPath, row.path_in_archive)), row.path_in_archive).toBe(true);
      }
    }
  }
  const modifiedFiles = archiveManifests.modified_coordinates.filter((row) => row.status === "included");
  expect(modifiedFiles.map((row) => row.file_id), "the held coordinate file is compared, not only empty lists").toEqual([
    multiconformerA.modelFile.artifactId,
  ]);

  // and the panel's script link opens the instructions, written out for this selection
  await selectionLine.getByRole("link", { name: "Use the script" }).click();
  await expect(page).toHaveURL(new RegExp(`/download\\?selection=${selectionId}#script$`));
  await expect(page.locator("[data-example=script]")).toContainText(`--selection ${selectionId} \\\n  --files all_files`);
  await expect(page.locator("[data-reuse-terms]")).toHaveText(REUSE_TERMS);
});

/** One entry with a model whose file our storage holds, checked; returns what the tests need. */
async function storedModel(
  submitterApi: Parameters<typeof createActiveEntry>[0],
  reviewerApi: Parameters<typeof createActiveEntry>[1],
  runTag: string,
  title: string,
) {
  // The model carries its entry's PDB ID, as the grouping rule requires (researchers' Q11).
  const pdbId = freshPdbId();
  const entry = await createActiveEntry(submitterApi, reviewerApi, `${title} entry ${runTag}`, {
    external_refs: { pdb: pdbId },
  });
  const content = `REMARK ${title} ${runTag}\nEND\n`;
  const created = await createActiveModelWithFile(submitterApi, reviewerApi, entry, {
    title: `${title} ${runTag}`,
    modelType: "Multiconformer",
    rFree: 0.16,
    pdbId,
    modelFileContent: content,
  });
  await storeFile(created.modelFile.uri, content);
  markStoredFileVerified(created.modelFile.artifactId, created.modelFile.sha256, created.modelFile.sizeBytes);
  return { entry, pdbId, ...created };
}

/** Selects one model from its PDB ID search and opens the drawer with a file choice. */
async function selectAndOpen(page: Page, pdbId: string, title: string) {
  await page.goto(`/browse?pdb_id=${pdbId}`);
  await page.getByRole("checkbox", { name: new RegExp(`Select ${title}`) }).check();
  await page.getByRole("button", { name: "Review and download" }).click();
  const drawer = page.getByRole("dialog", { name: "Download selection" });
  await drawer.getByRole("radio", { name: /Modified coordinate files/ }).check();
  return drawer;
}

test("should show the size limit before anything is built, with a smaller selection or the script as the ways out", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given a selection whose files add up to more than 10 GB: a stored file recorded at
  // 11 GB, checked; the preview adds recorded sizes up and reads no bytes
  const pdbId = freshPdbId();
  const entry = await createActiveEntry(submitterApi, reviewerApi, `L2 over limit entry ${runTag}`, {
    external_refs: { pdb: pdbId },
  });
  const large = await createActiveModelWithFile(submitterApi, reviewerApi, entry, {
    title: `L2 over limit ${runTag}`,
    modelType: "Multiconformer",
    rFree: 0.16,
    pdbId,
    modelFileSizeBytes: 11_000_000_000,
  });
  markStoredFileVerified(large.modelFile.artifactId, large.modelFile.sha256, large.modelFile.sizeBytes);
  const drawer = await selectAndOpen(page, pdbId, `L2 over limit ${runTag}`);

  // when the reader checks the archive
  await drawer.getByRole("button", { name: "Check the archive" }).click();
  await expect(drawer.getByRole("heading", { name: /Archive:/ })).toBeVisible();
  await runArchiveWorker();

  // then the limit shows, with both ways out, and nothing can be built
  const notice = drawer.locator("[data-limit-notice]");
  await expect(notice).toContainText("Too large for one archive", { timeout: 30_000 });
  await expect(notice).toContainText("This archive would hold 11 GB; one archive holds at most 10 GB");
  await expect(notice.getByRole("link", { name: "Use the script" })).toHaveAttribute(
    "href",
    /^\/download\?selection=[0-9a-f-]{36}#script$/,
  );
  await expect(drawer.getByRole("button", { name: "Build the archive" })).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Download selection" })).toContainText("Archive too large");
  await notice.getByRole("button", { name: "Make the selection smaller" }).click();
  await expect(drawer.locator("#download-selection-review")).toBeFocused();
});

test("should name a file that could not be read and fetch only it again in a retry archive", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}, testInfo) => {
  // given two selected models: one whose stored copy cannot be read while the archive is
  // built, and one whose copy reads fine through the same endpoint
  const model = await storedModel(submitterApi, reviewerApi, runTag, "L2 retry");
  const healthy = await storedModel(submitterApi, reviewerApi, runTag, "L2 healthy");
  await page.goto(`/browse?pdb_id=${model.pdbId}&pdb_id=${healthy.pdbId}`);
  await page.getByRole("button", { name: "Select all 2 results" }).click();
  await page.getByRole("button", { name: "Review and download" }).click();
  const drawer = page.getByRole("dialog", { name: "Download selection" });
  await drawer.getByRole("radio", { name: /Modified coordinate files/ }).check();
  await drawer.getByRole("button", { name: "Check the archive" }).click();
  await expect(drawer.getByRole("heading", { name: /Archive:/ })).toBeVisible();
  await runArchiveWorker();
  await drawer.getByRole("button", { name: "Build the archive" }).click({ timeout: 30_000 });
  const failingS3 = await startFailingS3(`/${model.modelFile.artifactId}/`);
  try {
    await runArchiveWorker({
      // Reads go through the failing endpoint; keys and archive links still resolve
      // against the stand-in itself, as the public (CloudFront) base.
      DYNAMIC_PDB_CDN_S3_ENDPOINT: failingS3.url,
      DYNAMIC_PDB_CDN_CLOUDFRONT_BASE_URL: `${stack.s3URL}/${stack.s3Bucket}`,
      DYNAMIC_PDB_ARCHIVE_WORKER_FILE_READ_ATTEMPTS: "1",
    });
  } finally {
    await failingS3.close();
  }

  // then the ready archive holds the healthy file, names the other, says why, and offers to
  // fetch only it again
  const first = archiveCards(page).first();
  const failure = first.locator(`[data-failure="${model.modelFile.artifactId}"]`);
  await expect(failure).toContainText("model.pdb", { timeout: 30_000 });
  await expect(failure).toContainText("could not be read");
  await expect(failure).toContainText("a retry may fetch it");
  await expect(first.locator(`[data-failure="${healthy.modelFile.artifactId}"]`)).toHaveCount(0);
  await expect(first.locator('[data-count="included"]')).toContainText("1 in the zip");
  await expect(first.locator("[data-transfer-help]")).toContainText("Download stopped? Download again");
  const [firstDownload] = await Promise.all([
    page.waitForEvent("download"),
    first.getByRole("link", { name: /Download zip/ }).click(),
  ]);
  const firstZip = testInfo.outputPath("first.zip");
  await firstDownload.saveAs(firstZip);
  const firstEntries = unzipList(firstZip);
  const firstManifest = firstEntries.find((entry) => entry.endsWith("/manifest.tsv"));
  expect(firstManifest, firstEntries.join("\n")).toBeDefined();
  const firstRows = manifestRows(unzipFile(firstZip, firstManifest as string).toString("utf8")).filter(
    (row) => row.record_type === "file",
  );
  expect(new Map(firstRows.map((row) => [row.file_id, [row.status, row.reason_code]]))).toEqual(
    new Map([
      [healthy.modelFile.artifactId, ["included", ""]],
      [model.modelFile.artifactId, ["unavailable", "read_failed"]],
    ]),
  );

  // when the reader retries, and the worker builds the retry with the copy readable again
  await first.getByRole("button", { name: "Retry the 1 file that could not be read" }).click();
  const retry = archiveCards(page).nth(1);
  await expect(retry).toContainText("Retry of the files that could not be read");
  await runArchiveWorker();

  // then the retry archive holds that file alone, and the first archive stays listed
  const retryLink = retry.getByRole("link", { name: /Download zip/ });
  await expect(retryLink).toBeVisible({ timeout: 30_000 });
  await expect(first.getByRole("link", { name: /Download zip/ })).toBeVisible();
  const [download] = await Promise.all([page.waitForEvent("download"), retryLink.click()]);
  const zipPath = testInfo.outputPath("retry.zip");
  await download.saveAs(zipPath);
  const entries = unzipList(zipPath);
  const manifestEntry = entries.find((entry) => entry.endsWith("/manifest.tsv"));
  expect(manifestEntry, entries.join("\n")).toBeDefined();
  const fileRows = manifestRows(unzipFile(zipPath, manifestEntry as string).toString("utf8")).filter(
    (row) => row.record_type === "file",
  );
  expect(fileRows.map((row) => [row.file_id, row.status])).toEqual([[model.modelFile.artifactId, "included"]]);
  const heldEntry = entries.find((entry) => entry.endsWith(`/${fileRows[0].path_in_archive}`) || entry === fileRows[0].path_in_archive);
  expect(heldEntry, entries.join("\n")).toBeDefined();
  expect(createHash("sha256").update(unzipFile(zipPath, heldEntry as string)).digest("hex")).toBe(model.modelFile.sha256);
});
