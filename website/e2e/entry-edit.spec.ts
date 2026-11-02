import { createActiveEntry } from "./support/api";
import { signIn, submitter } from "./support/auth";
import { expect, test } from "./support/fixtures";

test.beforeEach(async ({ context }) => {
  await signIn(context, submitter);
});

test("should save a new in-review revision when an active entry is edited", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `T2-D edit ${runTag}`);
  await page.goto(`/entries/${entryId}/edit`);
  const description = page.getByLabel("Description", { exact: true });
  await expect(description).toHaveValue("Created by the browser tests.");

  // when
  await description.fill("Edited by the browser tests.");
  await page.getByLabel("Change summary", { exact: true }).fill("Reworded the description.");
  await page.getByRole("button", { name: "Save change" }).click();

  // then
  await page.waitForURL(`**/entries/${entryId}?change=1`);
  await expect(
    page.getByRole("status").filter({ hasText: "Your change is waiting for review." }),
  ).toBeVisible();
  const group = await submitterApi.inReviewGroup({ entryId });
  expect(group.entryRevisions).toHaveLength(1);
  const revision = await submitterApi.entryRevision(entryId, group.entryRevisions[0].id);
  expect(revision).toMatchObject({ state: "in_review", details: "Edited by the browser tests." });
  expect(revision.parent_revision_id).not.toBeNull();
});
