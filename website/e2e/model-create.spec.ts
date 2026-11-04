import type { Locator, Page } from "@playwright/test";

import { createActiveEntry } from "./support/api";
import { signIn, submitter } from "./support/auth";
import {
  coordinateFile,
  reflectionsFile,
  scoredCoordinateFile,
  type UploadFile,
} from "./support/files";
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

/** The list beside the disabled submit button: what still stops the save. */
function submitBlockers(page: Page): Locator {
  return page.getByRole("status").filter({ hasText: "Before you can add the model" });
}

test("should save the model for review when it has an uploaded coordinate file, a complete program and scores linked to it", async ({
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
  await page.getByRole("button", { name: "Add metrics" }).click();
  await page.getByLabel("R-work", { exact: true }).fill("0.196");
  await page.getByLabel("Computed by").selectOption({ label: "Step 1: phenix.refine 1.21.2" });

  // when
  await page.getByRole("button", { name: "Add model" }).click();

  // then the model waits for review. Its run is not readable until a reviewer
  // publishes it, but in enforce mode (checked by global-setup.ts) the backend
  // saves a model only together with a complete run, and scores only with
  // their software or their run.
  await page.waitForURL(`**/entries/${entryId}`);
  const group = await submitterApi.inReviewGroup({ entryId });
  expect(group.modelRevisions).toHaveLength(1);
  const { model_id: modelId, id: revisionId } = group.modelRevisions[0];
  const revision = await submitterApi.modelRevision(entryId, modelId, revisionId);
  expect(revision).toMatchObject({ state: "in_review", title });
  expect(revision.metrics).toEqual([
    expect.objectContaining({ key: "r_work", value: 0.196, run_id: expect.any(String) }),
  ]);
  // and the coordinate file is the one stored in the S3 stand-in
  const stored = revision.artifacts.find((artifact) => artifact.name === model.name);
  expect(stored?.uri.startsWith(`${stack.s3URL}/${stack.s3Bucket}/`)).toBe(true);
  const download = await request.get(stored?.uri ?? "");
  expect(download.ok()).toBe(true);
  expect(await download.body()).toEqual(model.buffer);
});

test("should list what the API requires until the program and the score software are complete", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given a model with a coordinate file and a score, and no program yet
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `T2-D required entry ${runTag}`);
  const model = coordinateFile(runTag);
  const reflections = reflectionsFile(runTag);
  await page.goto(`/entries/${entryId}/models/new`);
  await page.getByLabel("Title", { exact: true }).fill(`T2-D required model ${runTag}`);
  await uploadFiles(page, [model, reflections]);
  await page.getByRole("button", { name: "Add metrics" }).click();
  await page.getByLabel("R-work", { exact: true }).fill("0.196");
  const addModel = page.getByRole("button", { name: "Add model" });
  await expect(addModel).toBeDisabled();
  await expect(submitBlockers(page)).toContainText("Programs: add the program that made this model");
  await expect(submitBlockers(page)).toContainText(
    "Software: name the software that computed the scores, or choose the program step that did",
  );

  // when the program has only a name
  await page.getByRole("button", { name: "Add program" }).click();
  const step = page.getByRole("group", { name: "Step 1" });
  await step.getByLabel("Name").fill("phenix.refine");

  // then the list names what the API would refuse it for
  await expect(addModel).toBeDisabled();
  await expect(submitBlockers(page)).toContainText("Version (step 1): required");
  await expect(submitBlockers(page)).toContainText(
    "Inputs and outputs (step 1): choose at least one input file",
  );
  await expect(submitBlockers(page)).toContainText(
    "Inputs and outputs (step 1): choose at least one output file of this model",
  );

  // when the program and the score software are complete
  await step.getByLabel("Version").fill("1.21.2");
  await linkFile(step, "Inputs", reflections);
  await linkFile(step, "Outputs", model);
  await page.getByLabel("Software (required)").fill("MolProbity");

  // then the model saves, with the scores' software
  await expect(submitBlockers(page)).toHaveCount(0);
  await addModel.click();
  await page.waitForURL(`**/entries/${entryId}`);
  const group = await submitterApi.inReviewGroup({ entryId });
  const { model_id: modelId, id: revisionId } = group.modelRevisions[0];
  const revision = await submitterApi.modelRevision(entryId, modelId, revisionId);
  expect(revision.metrics).toEqual([
    expect.objectContaining({ key: "r_work", value: 0.196, software_name: "MolProbity" }),
  ]);
});

test("should fill no score from the archive's mmCIF dropped beside the model file", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given: the depositor's model file, then the archive's mmCIF as a run input
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `G4 archive input entry ${runTag}`);
  const model = coordinateFile(runTag);
  const archive = scoredCoordinateFile(runTag, `1abc-${runTag}.cif`, { rWork: "0.250", rFree: "0.290" });
  await page.goto(`/entries/${entryId}/models/new`);
  await uploadFiles(page, [model]);

  // when
  await uploadFiles(page, [archive]);

  // then: no scores and no authors were filled from it
  await expect(page.getByRole("button", { name: "Add metrics" })).toBeVisible();
  await expect(page.getByLabel("R-work", { exact: true })).toHaveCount(0);
  await expect(page.getByText(`from ${archive.name}`)).toHaveCount(0);
});

test("should fill scores and authors from the model file and save them as read from that file", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `G4 file scores entry ${runTag}`);
  const model = scoredCoordinateFile(runTag, `model-${runTag}.cif`, { rWork: "0.191", rFree: "0.232" });
  const reflections = reflectionsFile(runTag);
  const title = `G4 file scores ${runTag}`;
  await page.goto(`/entries/${entryId}/models/new`);
  await page.getByLabel("Title", { exact: true }).fill(title);

  // when
  await uploadFiles(page, [model, reflections]);

  // then
  // The badge is part of each field's label.
  await expect(page.getByRole("textbox", { name: `R-work from ${model.name}`, exact: true })).toHaveValue("0.191");
  await expect(page.getByRole("textbox", { name: `R-free from ${model.name}`, exact: true })).toHaveValue("0.232");
  await expect(page.getByText(`from ${model.name}`, { exact: true })).toHaveCount(3);

  // when
  await page.getByRole("button", { name: "Add program" }).click();
  const step = page.getByRole("group", { name: "Step 1" });
  await step.getByLabel("Name").fill("phenix.refine");
  await step.getByLabel("Version").fill("1.21.2");
  await linkFile(step, "Inputs", reflections);
  await linkFile(step, "Outputs", model);
  await page.getByLabel("Computed by").selectOption({ label: "Step 1: phenix.refine 1.21.2" });
  await page.getByRole("button", { name: "Add model" }).click();

  // then
  await page.waitForURL(`**/entries/${entryId}`);
  const group = await submitterApi.inReviewGroup({ entryId });
  const { model_id: modelId, id: revisionId } = group.modelRevisions[0];
  const revision = await submitterApi.modelRevision(entryId, modelId, revisionId);
  const stored = revision.artifacts.find((artifact) => artifact.name === model.name);
  const fromFile = { source: "file", artifact_id: stored?.id, sha256: stored?.sha256 };
  expect(stored?.sha256).toMatch(/^[0-9a-f]{64}$/);
  expect(revision.field_sources).toEqual({
    "/metrics/r_work": fromFile,
    "/metrics/r_free": fromFile,
    "/metadata/authors": fromFile,
  });
});
