import { signIn, submitter } from "./support/auth";
import { expect, test } from "./support/fixtures";

test.beforeEach(async ({ context }) => {
  await signIn(context, submitter);
});

test("should keep Create entry disabled when a required field is empty", async ({ page, runTag }) => {
  // given
  await page.goto("/entries/new");
  const createEntry = page.getByRole("button", { name: "Create entry" });
  await expect(createEntry).toBeDisabled();

  // when
  await page.getByLabel("Title", { exact: true }).fill(`T2-D required ${runTag}`);

  // then
  await expect(createEntry).toBeDisabled();

  // when
  await page.getByLabel("Method", { exact: true }).fill("X-ray crystallography");

  // then
  await expect(createEntry).toBeEnabled();
});

test("should list why Create entry is disabled when required fields are empty", async ({ page, runTag }) => {
  // given
  await page.goto("/entries/new");
  const reasons = page.getByRole("status").filter({ hasText: "Before you can create the entry:" });

  // then
  await expect(reasons).toContainText("Title: required");
  await expect(reasons).toContainText("Method: required");

  // when
  await page.getByLabel("Title", { exact: true }).fill(`T2-D reasons ${runTag}`);

  // then
  await expect(reasons).not.toContainText("Title: required");
  await expect(reasons).toContainText("Method: required");

  // when
  await page.getByLabel("Method", { exact: true }).fill("X-ray crystallography");

  // then
  await expect(reasons).toBeHidden();
});

test("should save the entry for review when the filled-in form is submitted", async ({
  page,
  submitterApi,
  runTag,
}) => {
  // given
  const title = `T2-D new entry ${runTag}`;
  await page.goto("/entries/new");
  await page.getByLabel("Title", { exact: true }).fill(title);
  await page.getByLabel("Description", { exact: true }).fill("Saved by the browser tests.");
  await page.getByLabel("Method", { exact: true }).fill("X-ray crystallography");
  await page.getByLabel("Resolution (Å)", { exact: true }).fill("1.8");

  // when
  await page.getByRole("button", { name: "Create entry" }).click();

  // then
  await page.waitForURL("**/entries?tab=under-review");
  await expect(page.getByRole("link", { name: title })).toBeVisible();
  const group = await submitterApi.inReviewGroup({ title });
  expect(group.entryRevisions).toHaveLength(1);
  const revision = await submitterApi.entryRevision(group.entryId, group.entryRevisions[0].id);
  expect(revision).toMatchObject({
    state: "in_review",
    title,
    details: "Saved by the browser tests.",
    method: "X-ray crystallography",
    resolution: 1.8,
  });
});
