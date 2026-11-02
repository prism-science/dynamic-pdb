import type { Locator, Page } from "@playwright/test";

import { createActiveEntry } from "./support/api";
import { signIn, submitter } from "./support/auth";
import { coordinateFile, reflectionsFile, type UploadFile } from "./support/files";
import { expect, test } from "./support/fixtures";
import { stack } from "./support/stack";

test.beforeEach(async ({ context }) => {
  await signIn(context, submitter);
});

/**
 * Chooses files in the model form and waits until each has gone through the
 * real upload: presign at the backend, PUT to the S3 stand-in, complete.
 */
async function uploadFiles(page: Page, files: UploadFile[]): Promise<void> {
  const completed = files.map((file) =>
    page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/files/complete" &&
        (response.request().postData() ?? "").includes(file.name),
    ),
  );
  await page.locator('input[type="file"][multiple]').setInputFiles(files);
  const responses = await Promise.all(completed);
  responses.forEach((response, index) => {
    expect(response.ok(), `completing the upload of ${files[index].name}`).toBe(true);
  });
}

/** Links a file to one side of a program step by typing its name into the picker. */
async function linkFile(step: Locator, side: "Inputs" | "Outputs", file: UploadFile) {
  const picker = step.getByRole("textbox", { name: side, exact: true });
  await picker.fill(file.name);
  await picker.press("Enter");
  await expect(step.getByRole("button", { name: `Unlink ${file.name}` })).toBeVisible();
}

test("should save the model for review when it has an uploaded coordinate file and a complete program", async ({
  page,
  request,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `T2-D model entry ${runTag}`);
  const model = coordinateFile(runTag);
  const reflections = reflectionsFile(runTag);
  const title = `T2-D model ${runTag}`;
  await page.goto(`/entries/${entryId}/models/new`);
  await page.getByLabel("Title", { exact: true }).fill(title);
  await uploadFiles(page, [model, reflections]);
  await page.getByRole("button", { name: "Add program" }).click();
  const step = page.getByRole("group", { name: "Step 1" });
  await step.getByLabel("Name").fill("phenix.refine");
  await step.getByLabel("Version").fill("1.21.2");
  await linkFile(step, "Inputs", reflections);
  await linkFile(step, "Outputs", model);

  // when
  await page.getByRole("button", { name: "Add model" }).click();

  // then the model waits for review. Its run is not readable until a reviewer
  // publishes it, but in enforce mode (checked by global-setup.ts) the backend
  // saves a model only together with a complete run.
  await page.waitForURL(`**/entries/${entryId}`);
  const group = await submitterApi.inReviewGroup({ entryId });
  expect(group.modelRevisions).toHaveLength(1);
  const { model_id: modelId, id: revisionId } = group.modelRevisions[0];
  const revision = await submitterApi.modelRevision(entryId, modelId, revisionId);
  expect(revision).toMatchObject({ state: "in_review", title });
  // and the coordinate file is the one stored in the S3 stand-in
  const stored = revision.artifacts.find((artifact) => artifact.name === model.name);
  expect(stored?.uri.startsWith(`${stack.s3URL}/${stack.s3Bucket}/`)).toBe(true);
  const download = await request.get(stored?.uri ?? "");
  expect(download.ok()).toBe(true);
  expect(await download.body()).toEqual(model.buffer);
});

test("should mark the field beside the error when the backend rejects the model", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given a program with a name only: the form lets it through, and the
  // backend in enforce mode answers 422 at /model/runs/0/software_version
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `T2-D 422 entry ${runTag}`);
  await page.goto(`/entries/${entryId}/models/new`);
  await page.getByLabel("Title", { exact: true }).fill(`T2-D rejected model ${runTag}`);
  await uploadFiles(page, [coordinateFile(runTag)]);
  await page.getByRole("button", { name: "Add program" }).click();
  const step = page.getByRole("group", { name: "Step 1" });
  await step.getByLabel("Name").fill("phenix.refine");
  const addModel = page.getByRole("button", { name: "Add model" });
  await expect(addModel).toBeEnabled();

  // when
  await addModel.click();

  // then
  await expect(
    page.getByText("Dynamic PDB did not accept some values. They are marked below."),
  ).toBeVisible();
  await expect(step.locator("label").filter({ hasText: /^Version/ })).toContainText("Is required.");
  await expect(page).toHaveURL(new RegExp(`/entries/${entryId}/models/new$`));
  const groups = await submitterApi.inReviewGroups();
  expect(groups.find((item) => item.entryId === entryId)).toBeUndefined();
});
