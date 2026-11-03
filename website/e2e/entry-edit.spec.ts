import { createActiveEntry } from "./support/api";
import { signIn, submitter } from "./support/auth";
import { ARCHIVE_VERSION, freshPdbId } from "./support/fakeRcsb";
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

test("should show where each archive field came from and save an own value as the depositor's", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given: an active entry whose title the depositor typed and whose method and
  // resolution equal the fake archive's, so the server recorded them as rcsb
  const pdbId = freshPdbId();
  const title = `T3-D edit ${runTag}`;
  const { entryId, revisionId } = await submitterApi.createEntry({
    title,
    method: "X-ray crystallography",
    resolution: 1.5,
    external_refs: { pdb: pdbId },
  });
  const created = await submitterApi.entryRevision(entryId, revisionId);
  expect(created.field_sources?.["/title"]).toEqual({ source: "depositor" });
  expect(created.field_sources?.["/metadata/resolution"]).toMatchObject({ source: "rcsb" });
  await reviewerApi.approveEntryRevision(entryId, revisionId);
  await page.goto(`/entries/${entryId}/edit`);

  // then
  await expect(page.getByText("set by the depositor", { exact: true })).toHaveCount(1);
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue(title);
  await expect(
    page.getByText(`from PDB ${pdbId} · archive ${ARCHIVE_VERSION}`, { exact: true }),
  ).toHaveCount(2);
  await expect(page.getByLabel("Resolution (Å)", { exact: true })).toHaveCount(0);

  // when
  await page.getByRole("button", { name: "Use my own value for Resolution (Å)" }).click();

  // then: the stored value is unchanged, so it would stay the archive's
  const resolution = page.getByLabel("Resolution (Å)", { exact: true });
  await expect(resolution).toHaveValue("1.5");
  await expect(
    page.getByText(
      "Same as the PDB value, so it stays the PDB's and the weekly update keeps it current.",
      { exact: true },
    ),
  ).toBeVisible();

  // when
  await resolution.fill("2.1");

  // then
  await expect(
    page.getByText(
      "The weekly PDB update will no longer change these fields, because you set them yourself: Resolution.",
      { exact: true },
    ),
  ).toBeVisible();

  // when
  await page.getByRole("button", { name: "Save change" }).click();

  // then
  await page.waitForURL(`**/entries/${entryId}?change=1`);
  const group = await submitterApi.inReviewGroup({ entryId });
  const revision = await submitterApi.entryRevision(entryId, group.entryRevisions[0].id);
  expect(revision.resolution).toBe(2.1);
  expect(revision.field_sources?.["/metadata/resolution"]).toEqual({ source: "depositor" });
  expect(revision.field_sources?.["/title"]).toEqual({ source: "depositor" });
  expect(revision.field_sources?.["/metadata/method"]).toMatchObject({
    source: "rcsb",
    accession: pdbId,
    release: ARCHIVE_VERSION,
  });
});
