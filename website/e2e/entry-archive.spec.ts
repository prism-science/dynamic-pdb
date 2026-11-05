import { signIn, submitter } from "./support/auth";
import {
  ARCHIVE_VERSION,
  answerArchiveEntryMalformed,
  archiveTitle,
  failArchiveEntry,
  freshPdbId,
  restoreArchiveEntry,
  setArchiveEntityFields,
  setArchiveEntryFields,
} from "./support/fakeRcsb";
import { expect, test } from "./support/fixtures";

// The New entry form with a PDB ID: the archive's values, the depositor's own
// values beside them, and where the server records each one came from. The
// archive is the fake RCSB (e2e/stack/fake-rcsb.mjs).

// website/src/app/components/entry-form/types.ts
const entryDraftStorageKey = "dpdb:new-entry-draft";

// The archive fields the fake fills, by JSON pointer (ENTRY_ARCHIVE_POINTERS).
const filledPointers = [
  "/title",
  "/metadata/details",
  "/metadata/method",
  "/metadata/resolution",
  "/metadata/space_group",
  "/metadata/crystallography",
];

test.beforeEach(async ({ context }) => {
  await signIn(context, submitter);
});

test("should fill the archive's values with their source and show a different typed title", async ({
  page,
}) => {
  // given
  const pdbId = freshPdbId();
  await page.goto("/entries/new");
  await page.getByLabel("Title", { exact: true }).fill("Typed before the lookup");

  // when
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);

  // then
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();
  await expect(
    page.getByText(`from PDB ${pdbId} · archive ${ARCHIVE_VERSION}`, { exact: true }),
  ).toHaveCount(filledPointers.length);
  await expect(page.getByText("You typed: “Typed before the lookup”", { exact: true })).toBeVisible();
  // The archive's title is shown read-only until the depositor takes it over.
  await expect(page.getByLabel("Title", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Use my own value for Title" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create entry" })).toBeEnabled();
});

test("should save an own title as the depositor's and the other filled values as the archive's", async ({
  page,
  submitterApi,
  runTag,
}) => {
  // given
  const pdbId = freshPdbId();
  const ownTitle = `T3-D own title ${runTag}`;
  await page.goto("/entries/new");
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();

  // when
  await page.getByRole("button", { name: "Use my own value for Title" }).click();
  const title = page.getByLabel("Title", { exact: true });
  await expect(title).toHaveValue(archiveTitle(pdbId));
  await title.fill(ownTitle);

  // then
  await expect(page.getByText(`PDB ${pdbId}: ${archiveTitle(pdbId)}`, { exact: true })).toBeVisible();
  await expect(
    page.getByText(
      "The weekly PDB update will no longer change these fields, because you set them yourself: Title.",
      { exact: true },
    ),
  ).toBeVisible();

  // when
  await page.getByRole("button", { name: "Create entry" }).click();

  // then
  await page.waitForURL("**/entries?tab=under-review");
  const group = await submitterApi.inReviewGroup({ title: ownTitle });
  const revision = await submitterApi.entryRevision(group.entryId, group.entryRevisions[0].id);
  expect(revision.title).toBe(ownTitle);
  expect(revision.field_sources?.["/title"]).toEqual({ source: "depositor" });
  for (const pointer of filledPointers.filter((each) => each !== "/title")) {
    expect(revision.field_sources?.[pointer], pointer).toMatchObject({
      source: "rcsb",
      accession: pdbId,
      release: ARCHIVE_VERSION,
    });
  }
});

test("should keep an own value and the choice to use it when the page is reloaded", async ({
  page,
}) => {
  // given
  const pdbId = freshPdbId();
  await page.goto("/entries/new");
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);
  await page.getByRole("button", { name: "Use my own value for Resolution (Å)" }).click();
  await page.getByLabel("Resolution (Å)", { exact: true }).fill("2.25");
  await expect
    .poll(() => page.evaluate((key) => localStorage.getItem(key) ?? "", entryDraftStorageKey))
    .toContain('"ownFields":["resolution"]');

  // when
  await page.reload();
  const prompt = page.getByRole("dialog");
  await expect(prompt.getByRole("heading", { name: "Continue where you left off?" })).toBeVisible();
  await prompt.getByRole("button", { name: "Continue" }).click();

  // then
  await expect(page.getByLabel("Resolution (Å)", { exact: true })).toHaveValue("2.25");
  await expect(page.getByText(`PDB ${pdbId}: 1.5`, { exact: true })).toBeVisible();
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();

  // when
  await page.getByRole("button", { name: "Use the PDB value for Resolution (Å)" }).click();

  // then
  await expect(page.getByLabel("Resolution (Å)", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Use my own value for Resolution (Å)" })).toBeVisible();
});

test("should keep the typed values and fill the archive's when a failed lookup is retried", async ({
  page,
}) => {
  // given
  const pdbId = freshPdbId();
  await failArchiveEntry(pdbId);
  await page.goto("/entries/new");
  await page.getByLabel("Title", { exact: true }).fill("Typed before the lookup");

  // when
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);

  // then
  await expect(page.getByText(`Could not read ${pdbId} from the PDB`)).toBeVisible();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Typed before the lookup");

  // when
  await restoreArchiveEntry(pdbId);
  await page.getByRole("button", { name: "Try again" }).click();

  // then
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();
  await expect(page.getByText("You typed: “Typed before the lookup”", { exact: true })).toBeVisible();

  // when
  await page.getByRole("button", { name: "Use my own value for Title" }).click();

  // then
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Typed before the lookup");
});

test("should show the server's error beside an archive value it rejects and save an own value instead", async ({
  page,
  submitterApi,
  runTag,
}) => {
  // given: an archive resolution the server's validation rejects
  const pdbId = freshPdbId();
  await setArchiveEntryFields(pdbId, { rcsb_entry_info: { resolution_combined: [0] } });
  await page.goto("/entries/new");
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();
  // The lookup already says so, as advice: it does not block the submit.
  await expect(
    page.getByText(`The value from PDB ${pdbId} breaks a schema rule: must be greater than 0.`, {
      exact: true,
    }),
  ).toBeVisible();

  // when
  await page.getByRole("button", { name: "Create entry" }).click();

  // then
  await expect(page.getByText("Must be greater than 0.", { exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/entries\/new$/);

  // when
  await page.getByRole("button", { name: "Use my own value for Title" }).click();
  await page.getByLabel("Title", { exact: true }).fill(`T3-D rejected archive value ${runTag}`);
  await page.getByRole("button", { name: "Use my own value for Resolution (Å)" }).click();
  await page.getByLabel("Resolution (Å)", { exact: true }).fill("1.9");
  await page.getByRole("button", { name: "Create entry" }).click();

  // then
  await page.waitForURL("**/entries?tab=under-review");
  const group = await submitterApi.inReviewGroup({ title: `T3-D rejected archive value ${runTag}` });
  const revision = await submitterApi.entryRevision(group.entryId, group.entryRevisions[0].id);
  expect(revision.resolution).toBe(1.9);
  expect(revision.field_sources?.["/metadata/resolution"]).toEqual({ source: "depositor" });
});

test("should keep an archive method that is not one a depositor may pick", async ({
  page,
  submitterApi,
  runTag,
}) => {
  // given: a PDB entry whose method is outside the depositor list
  const pdbId = freshPdbId();
  const title = `G6 archive method ${runTag}`;
  await setArchiveEntryFields(pdbId, { exptl: [{ method: "NEUTRON DIFFRACTION" }] });
  await page.goto("/entries/new");
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);
  await expect(page.getByText("NEUTRON DIFFRACTION", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Use my own value for Title" }).click();
  await page.getByLabel("Title", { exact: true }).fill(title);

  // when
  await page.getByRole("button", { name: "Create entry" }).click();

  // then
  await page.waitForURL("**/entries?tab=under-review");
  const group = await submitterApi.inReviewGroup({ title });
  const revision = await submitterApi.entryRevision(group.entryId, group.entryRevisions[0].id);
  expect(revision.method).toBe("NEUTRON DIFFRACTION");
  expect(revision.field_sources?.["/metadata/method"]).toMatchObject({ source: "rcsb", accession: pdbId });
});

test("should say beside the PDB ID that another entry already has it", async ({
  page,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given: an active entry for the PDB ID
  const pdbId = freshPdbId();
  const { entryId, revisionId } = await submitterApi.createEntry({
    title: `G6 entry that has the PDB ID ${runTag}`,
    method: "X-ray crystallography",
    external_refs: { pdb: pdbId },
  });
  await reviewerApi.approveEntryRevision(entryId, revisionId);
  await page.goto("/entries/new");
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();

  // when
  await page.getByRole("button", { name: "Create entry" }).click();

  // then
  await expect(
    page.getByText("Dynamic PDB did not accept some values. They are marked below.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Another entry already has this PDB ID; add your models to that entry instead.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByText(/Backend responded|\/entry\//)).toHaveCount(0);
  await expect(page).toHaveURL(/\/entries\/new$/);
});

test("should show a value RCSB reports twice beside its field, without blocking the submit", async ({
  page,
}) => {
  // given
  const pdbId = freshPdbId();
  await setArchiveEntryFields(pdbId, {
    exptl: [{ method: "X-RAY DIFFRACTION" }, { method: "NEUTRON DIFFRACTION" }],
  });
  await page.goto("/entries/new");

  // when
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);

  // then
  await expect(
    page.getByText(
      `PDB ${pdbId} lists more than one method: X-RAY DIFFRACTION, NEUTRON DIFFRACTION. The form fills in the first.`,
      { exact: true },
    ),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Create entry" })).toBeEnabled();
});

test("should mark unclear archive mutations and typed mutations without a UniProt mapping for review", async ({
  page,
}) => {
  // given: entity 1 of the fake archive has no UniProt mapping
  const pdbId = freshPdbId();
  await setArchiveEntityFields(pdbId, "1", {
    rcsb_polymer_entity: { pdbx_description: "Lysozyme C", pdbx_mutation: "C54T, ENGINEERED MUTATION" },
  });
  await page.goto("/entries/new");

  // when
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);

  // then
  await expect(page.getByText(`PDB ${pdbId}: C54T, ENGINEERED MUTATION`)).toBeVisible();
  await expect(page.getByText("needs review", { exact: true })).toBeVisible();
  await expect(
    page.getByText(
      "Part of the PDB's mutation text is not a single substitution, so it needs review: type the substitutions you mean.",
      { exact: true },
    ),
  ).toBeVisible();

  // when
  await page.getByLabel("Mutations", { exact: true }).fill("C54T");
  await page.getByLabel("Mutations", { exact: true }).press("Enter");

  // then
  await expect(
    page.getByText(
      "Needs review: the PDB maps this entity to no UniProt entry, so these positions cannot be checked against UniProt numbering.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Create entry" })).toBeEnabled();
});

test("should keep the draft and allow a retry when RCSB answers with malformed data", async ({
  page,
}) => {
  // given
  const pdbId = freshPdbId();
  await answerArchiveEntryMalformed(pdbId);
  await page.goto("/entries/new");
  await page.getByLabel("Title", { exact: true }).fill("Typed before the lookup");

  // when
  await page.getByLabel("PDB ID", { exact: true }).fill(pdbId);

  // then
  await expect(page.getByText(`Could not read ${pdbId} from the PDB`)).toBeVisible();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Typed before the lookup");

  // when: the draft comes back after a reload, and its lookup fails the same way
  await page.reload();
  await page.getByRole("button", { name: "Continue" }).click();

  // then
  await expect(page.getByText(`Could not read ${pdbId} from the PDB`)).toBeVisible();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Typed before the lookup");

  // when: RCSB answers properly again; the bad answer was not cached
  await restoreArchiveEntry(pdbId);
  await page.getByRole("button", { name: "Try again" }).click();

  // then
  await expect(page.getByText(archiveTitle(pdbId), { exact: true })).toBeVisible();
  await expect(page.getByText("You typed: “Typed before the lookup”", { exact: true })).toBeVisible();
});
