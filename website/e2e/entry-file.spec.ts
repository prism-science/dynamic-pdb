import type { Page } from "@playwright/test";

import { reviewer, signIn, submitter } from "./support/auth";
import { archiveTitle, freshPdbId } from "./support/fakeRcsb";
import { entryCoordinateFile, type UploadFile } from "./support/files";
import { expect, test } from "./support/fixtures";

// The New entry form reads an uploaded mmCIF on the server (POST
// /v1/files/extract, on in local.yml) before the entry is saved, fills what the
// depositor left empty, and saves those values as read from the file.

/** Uploads files through the entry form and waits until each upload is complete. */
async function uploadEntryFiles(page: Page, files: UploadFile[]): Promise<void> {
  const completed = files.map((file) =>
    page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/files/complete" &&
        (response.request().postData() ?? "").includes(file.name),
    ),
  );
  await page.locator('input[type="file"][multiple]').setInputFiles(files);
  await Promise.all(completed);
}

test("should fill the entry from an uploaded mmCIF read on the server and save the values as the file's", async ({
  context,
  page,
  submitterApi,
  runTag,
}) => {
  // given
  await signIn(context, submitter);
  const file = entryCoordinateFile(runTag, `deposit-${runTag}.cif`, {
    resolution: "1.85",
    method: "X-RAY DIFFRACTION",
    spaceGroup: "P 21 21 21",
  });
  const title = `G5 entry from file ${runTag}`;
  await page.goto("/entries/new");
  await page.getByLabel("Title", { exact: true }).fill(title);

  // when
  await uploadEntryFiles(page, [file]);

  // then
  await expect(page.getByLabel("Resolution (Å)", { exact: true })).toHaveValue("1.85");
  await expect(page.getByLabel("Method", { exact: true })).toHaveValue("X-ray crystallography");
  await expect(page.getByLabel("Space group", { exact: true })).toHaveValue("P 21 21 21");
  await expect(page.getByText(`from ${file.name}`, { exact: true })).toHaveCount(3);

  // when
  await page.getByRole("button", { name: "Create entry" }).click();

  // then
  await page.waitForURL("**/entries?tab=under-review");
  const group = await submitterApi.inReviewGroup({ title });
  const revision = await submitterApi.entryRevision(group.entryId, group.entryRevisions[0].id);
  expect(revision.resolution).toBe(1.85);
  const resolutionSource = revision.field_sources?.["/metadata/resolution"];
  expect(resolutionSource).toMatchObject({ source: "file", artifact_id: expect.any(String) });
  expect(resolutionSource?.sha256).toMatch(/^[0-9a-f]{64}$/);
  expect(revision.field_sources?.["/metadata/method"]).toMatchObject({ source: "file" });
  expect(revision.field_sources?.["/metadata/space_group"]).toMatchObject({ source: "file" });
  expect(revision.field_sources?.["/title"]).toEqual({ source: "depositor" });
});

test("should offer to look up the PDB ID an uploaded mmCIF names", async ({ context, page, runTag }) => {
  // given
  await signIn(context, submitter);
  const pdbId = freshPdbId();
  const file = entryCoordinateFile(runTag, `${pdbId.toLowerCase()}-${runTag}.cif`, {
    entryId: pdbId,
    resolution: "1.5",
    method: "X-RAY DIFFRACTION",
    spaceGroup: "P 1",
  });
  await page.goto("/entries/new");

  // when
  await uploadEntryFiles(page, [file]);
  await page.getByRole("button", { name: `Look up PDB ${pdbId}, named in ${file.name}` }).click();

  // then
  await expect(page.getByLabel("PDB ID", { exact: true })).toHaveValue(pdbId);
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();
});

test("should keep the draft and offer to read the file again when the server cannot read it", async ({
  context,
  page,
  reviewerApi,
  runTag,
}) => {
  // given: the reviewer has used up their extraction requests for this minute
  await signIn(context, reviewer);
  for (let attempt = 0; attempt < 10; attempt += 1) {
    await reviewerApi.post("/v1/files/extract", {
      artifact_id: crypto.randomUUID(),
      object_url: "https://files.example/used-up.cif",
      sha256: "not-a-checksum",
    });
  }
  const file = entryCoordinateFile(runTag, `busy-${runTag}.cif`, {
    resolution: "2.0",
    method: "X-RAY DIFFRACTION",
    spaceGroup: "P 1",
  });
  await page.goto("/entries/new");
  await page.getByLabel("Title", { exact: true }).fill("Typed before the upload");

  // when
  await uploadEntryFiles(page, [file]);

  // then: after one automatic retry the form says so and keeps the draft
  await expect(
    page.getByText(
      `Could not read ${file.name} on the server: the server is busy reading other files. The form keeps what it read in the browser.`,
    ),
  ).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole("button", { name: `Read ${file.name} again` })).toBeVisible();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Typed before the upload");
  await expect(page.getByLabel("Resolution (Å)", { exact: true })).toHaveValue("");
});
