import { createActiveEntry } from "./support/api";
import { reviewer, signIn } from "./support/auth";
import { freshPdbId } from "./support/fakeRcsb";
import { expect, test } from "./support/fixtures";

// Researcher answer of 17 Nov, Q11: the "add a model from a PDB ID" form stays,
// but reviewers don't approve a PDB deposition filed under another entry until
// the grouping rules pass. The API refuses it; the reviewer's card says why
// before Approve is pressed, and Reject stays available.

test("should hold back Approve, and say why, for a PDB deposition filed under another entry", async ({
  browser,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given an entry and a re-refinement of another PDB ID filed under it, in review
  const entryPdbId = freshPdbId();
  const filedPdbId = freshPdbId();
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `S8 filing ${runTag}`, {
    external_refs: { pdb: entryPdbId },
  });
  await submitterApi.createModel(entryId, {
    title: `S8 filed re-refinement ${runTag}`,
    modelType: "Ensemble",
    rFree: 0.19,
    pdbId: filedPdbId,
    archive: true,
  });

  // when a reviewer opens it in the review queue
  const context = await browser.newContext();
  try {
    await signIn(context, reviewer);
    const page = await context.newPage();
    await page.goto(`/review?sel=${entryId}`);

    // then the card names both PDB IDs, Approve is held back and Reject is not
    const note = page.getByRole("note").filter({ hasText: `the model's PDB ID is ${filedPdbId}` });
    await expect(note).toContainText(`the entry's PDB ID is ${entryPdbId}`);
    await expect(note).toContainText("grouping rules");
    await expect(page.getByRole("button", { name: "Approve" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Reject" })).toBeEnabled();
  } finally {
    await context.close();
  }
});
