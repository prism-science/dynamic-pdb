import type { APIRequestContext } from "@playwright/test";

import { createActiveEntry, createActiveModel } from "./support/api";
import { freshPdbId } from "./support/fakeRcsb";
import { expect, test } from "./support/fixtures";
import { stack } from "./support/stack";

// Researcher answer 4: searching any PDB ID of an entry that holds several
// depositions finds that entry with the model of that PDB ID selected. Nothing
// is merged: the website only opens that model.

/** GET /v1/entries for a search: each entry's matched models. */
async function apiMatchedModels(request: APIRequestContext, query: string) {
  const response = await request.get(`${stack.backendURL}/v1/entries?${query}`);
  expect(response.status(), await response.text()).toBe(200);
  const body = (await response.json()) as {
    data: { id: string; attributes: { matched_models?: { model_id: string; pdb_id: string }[] } }[];
  };
  return body.data.map((item) => ({ entryId: item.id, matched: item.attributes.matched_models }));
}

test("should open the model of the searched PDB ID, from the header search and from the PDB ID filter", async ({
  page,
  request,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given: an entry with its deposited model, a re-refinement deposited from the
  // same data, and a computed model
  const entryPdbId = freshPdbId();
  const reRefinementPdbId = freshPdbId();
  const entryId = await createActiveEntry(submitterApi, reviewerApi, `S8 several depositions ${runTag}`, {
    external_refs: { pdb: entryPdbId },
  });
  const deposited = await createActiveModel(submitterApi, reviewerApi, entryId, {
    title: `S8 deposited ${runTag}`,
    modelType: "Deposited",
    rFree: 0.21,
    pdbId: entryPdbId,
  });
  const reRefinement = await createActiveModel(submitterApi, reviewerApi, entryId, {
    title: `S8 re-refinement ${runTag}`,
    modelType: "Ensemble",
    rFree: 0.19,
    pdbId: reRefinementPdbId,
    archive: true,
  });
  await createActiveModel(submitterApi, reviewerApi, entryId, {
    title: `S8 multiconformer ${runTag}`,
    modelType: "Multiconformer",
    rFree: 0.18,
  });

  // when the reader searches the re-refinement's PDB ID in the header
  await page.goto("/browse");
  await page.getByRole("searchbox", { name: "Search entries" }).fill(reRefinementPdbId.toLowerCase());
  await page.getByRole("searchbox", { name: "Search entries" }).press("Enter");

  // then the entry's card says which model it opens, as the API selected it
  await expect(page).toHaveURL(`/browse?query=${reRefinementPdbId.toLowerCase()}`);
  const card = page.getByRole("link", { name: new RegExp(`S8 several depositions ${runTag}`) });
  await expect(card).toHaveCount(1);
  await expect(card.getByText(`Opens model PDB ${reRefinementPdbId}`, { exact: true })).toBeVisible();
  await expect(card).toHaveAttribute("href", `/entries/${entryId}/models/${reRefinement}`);
  expect(await apiMatchedModels(request, `query=${reRefinementPdbId.toLowerCase()}`)).toEqual([
    { entryId, matched: [{ model_id: reRefinement, pdb_id: reRefinementPdbId }] },
  ]);

  // when the reader opens it, the entry page shows that model
  await card.click();
  await expect(page).toHaveURL(`/entries/${entryId}/models/${reRefinement}`);
  await expect(page.getByRole("heading", { level: 1 })).toContainText(`S8 re-refinement ${runTag}`);

  // and the entry's own PDB ID opens its deposited model
  await page.goto(`/browse?query=${entryPdbId}`);
  await expect(page.getByRole("link", { name: new RegExp(`S8 several depositions ${runTag}`) })).toHaveAttribute(
    "href",
    `/entries/${entryId}/models/${deposited}`,
  );

  // and the model search's PDB ID filter finds the same model, as GET /v1/models does
  await page.goto(`/browse?pdb_id=${reRefinementPdbId}`);
  await expect(page.getByText("1 model matches these filters", { exact: true })).toBeVisible();
  const row = page.locator(`[data-model-id="${reRefinement}"]`);
  await expect(row.getByRole("link", { name: `S8 re-refinement ${runTag}` })).toHaveAttribute(
    "href",
    `/entries/${entryId}/models/${reRefinement}`,
  );
  const apiResponse = await request.get(`${stack.backendURL}/v1/models?pdb_id=${reRefinementPdbId}`);
  expect(apiResponse.status()).toBe(200);
  const api = (await apiResponse.json()) as { data: { id: string }[] };
  expect(api.data.map((item) => item.id)).toEqual([reRefinement]);
});
