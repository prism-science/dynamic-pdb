import type { APIRequestContext, Locator, Page } from "@playwright/test";

import { createActiveEntry, createActiveModel } from "./support/api";
import { expect, test } from "./support/fixtures";
import { stack } from "./support/stack";

// The model search on /browse against GET /v1/models (filters are on in
// backend/config/local.yml). Each test makes its own entries with a resolution
// no other record has, so the searches see only them.

type SearchRecords = {
  resolution: string;
  /** Entry A: an Ensemble with R-free 0.30 and a Multiconformer with 0.15. */
  entryA: string;
  ensembleA: string;
  multiconformerA: string;
  /** Entry B: a Multiconformer with R-free 0.12. */
  entryB: string;
  multiconformerB: string;
};

/** A resolution, in Å, that no other record in the shared database has. */
function uniqueResolution(): string {
  return (3 + Math.random() * 0.9).toFixed(6);
}

async function createSearchRecords(
  submitterApi: Parameters<typeof createActiveEntry>[0],
  reviewerApi: Parameters<typeof createActiveEntry>[1],
  runTag: string,
): Promise<SearchRecords> {
  const resolution = uniqueResolution();
  const entryA = await createActiveEntry(submitterApi, reviewerApi, `S5 search A ${runTag}`, {
    resolution: Number(resolution),
  });
  const entryB = await createActiveEntry(submitterApi, reviewerApi, `S5 search B ${runTag}`, {
    resolution: Number(resolution),
  });
  const ensembleA = await createActiveModel(submitterApi, reviewerApi, entryA, {
    title: `S5 ensemble A ${runTag}`,
    modelType: "Ensemble",
    rFree: 0.3,
  });
  const multiconformerA = await createActiveModel(submitterApi, reviewerApi, entryA, {
    title: `S5 multiconformer A ${runTag}`,
    modelType: "Multiconformer",
    rFree: 0.15,
  });
  const multiconformerB = await createActiveModel(submitterApi, reviewerApi, entryB, {
    title: `S5 multiconformer B ${runTag}`,
    modelType: "Multiconformer",
    rFree: 0.12,
  });
  return { resolution, entryA, ensembleA, multiconformerA, entryB, multiconformerB };
}

/** The model IDs of the result rows on the page, in order. */
async function resultModelIds(page: Page): Promise<string[]> {
  const rows = page.locator("[data-model-id]");
  return rows.evaluateAll((elements) => elements.map((element) => element.getAttribute("data-model-id") ?? ""));
}

/** The same search through the API: the model IDs in order and meta.total. */
async function apiSearch(request: APIRequestContext, query: string) {
  const response = await request.get(`${stack.backendURL}/v1/models?${query}`);
  expect(response.status(), await response.text()).toBe(200);
  const body = (await response.json()) as {
    data: { id: string }[];
    meta?: { total?: { count: number; exact: boolean } };
  };
  return { ids: body.data.map((item) => item.id), total: body.meta?.total };
}

/** Opens a folded group of the panel, as a reader clicks its heading. */
async function openGroup(panel: Locator, name: string): Promise<void> {
  await panel.locator("summary", { hasText: name }).click();
}

function searchQuery(page: Page): string {
  return new URL(page.url()).search.replace(/^\?/, "");
}

test("should search with the panel, and restore the same search from a shared URL as the API runs it", async ({
  page,
  browser,
  request,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given
  const records = await createSearchRecords(submitterApi, reviewerApi, runTag);
  await page.goto("/browse");
  const panel = page.getByRole("search", { name: "Filter models" });

  // when
  await panel.getByLabel("Resolution minimum").fill(records.resolution);
  await panel.getByLabel("Resolution maximum").fill(records.resolution);
  await panel.getByRole("checkbox", { name: "Multiconformer", exact: true }).check();
  await panel.getByRole("button", { name: "Apply filters" }).click();

  // then the URL holds the search, in the API's own parameters
  await expect(page).toHaveURL(
    `/browse?resolution_min=${records.resolution}&resolution_max=${records.resolution}&model_type=Multiconformer`,
  );
  await expect(page.getByText("2 models match these filters", { exact: true })).toBeVisible();
  const ids = await resultModelIds(page);
  expect(ids.sort()).toEqual([records.multiconformerA, records.multiconformerB].sort());
  // and each row is labelled with its entry
  const rowA = page.locator(`[data-model-id="${records.multiconformerA}"]`);
  await expect(rowA.getByRole("link", { name: `S5 multiconformer A ${runTag}` })).toHaveAttribute(
    "href",
    `/entries/${records.entryA}/models/${records.multiconformerA}`,
  );
  await expect(rowA.getByRole("link", { name: records.entryA })).toHaveAttribute("href", `/entries/${records.entryA}`);
  await expect(rowA.getByText(`S5 search A ${runTag}`, { exact: true })).toBeVisible();
  await expect(rowA.getByText("R-free 0.150", { exact: true })).toBeVisible();

  // and the API, given the page's query string, returns the same models and count
  const query = searchQuery(page);
  const api = await apiSearch(request, query);
  expect(await resultModelIds(page)).toEqual(api.ids);
  expect(api.total).toEqual({ count: 2, exact: true });
  await expect(page.getByRole("link", { name: `GET ${stack.backendURL}/v1/models?${query}` })).toBeVisible();

  // and a new visitor with the same URL sees the same search
  const shared = await browser.newPage();
  try {
    await shared.goto(`/browse?${query}`);
    const sharedPanel = shared.getByRole("search", { name: "Filter models" });
    await expect(sharedPanel.getByLabel("Resolution minimum")).toHaveValue(records.resolution);
    await expect(sharedPanel.getByLabel("Resolution maximum")).toHaveValue(records.resolution);
    await expect(sharedPanel.getByRole("checkbox", { name: "Multiconformer", exact: true })).toBeChecked();
    await expect(sharedPanel.getByRole("checkbox", { name: "Ensemble", exact: true })).not.toBeChecked();
    await expect(shared.getByText("2 models match these filters", { exact: true })).toBeVisible();
    expect(await resultModelIds(shared)).toEqual(api.ids);
  } finally {
    await shared.close();
  }
});

test("should match score filters on the same model, never on another model of the entry", async ({
  page,
  request,
  submitterApi,
  reviewerApi,
  runTag,
}) => {
  // given entry A has an Ensemble at R-free 0.30 and a Multiconformer at 0.15
  const records = await createSearchRecords(submitterApi, reviewerApi, runTag);
  await page.goto(`/browse?resolution_min=${records.resolution}&resolution_max=${records.resolution}`);
  const panel = page.getByRole("search", { name: "Filter models" });
  await expect(page.getByText("3 models match these filters", { exact: true })).toBeVisible();

  // when
  await panel.getByRole("checkbox", { name: "Ensemble", exact: true }).check();
  await openGroup(panel, "Scores");
  await panel.getByLabel("R-free maximum").fill("0.2");
  await panel.getByRole("button", { name: "Apply filters" }).click();

  // then
  await expect(page).toHaveURL(/model_type=Ensemble&r_free_max=0\.2$/);
  await expect(page.getByText("No models match these filters.", { exact: true })).toBeVisible();
  expect(await resultModelIds(page)).toEqual([]);
  // and the search help, open on an empty result, explains missing values
  const results = page.getByRole("region", { name: "Model search results" });
  await expect(results.getByText("Missing values never match.", { exact: true })).toBeVisible();
  const api = await apiSearch(request, searchQuery(page));
  expect(api).toEqual({ ids: [], total: { count: 0, exact: true } });
});

test("should stop a range whose minimum is above its maximum, and show the API's reason for a URL it refuses", async ({
  page,
}) => {
  // given
  await page.goto("/browse");
  const panel = page.getByRole("search", { name: "Filter models" });

  // when
  await openGroup(panel, "Scores");
  await panel.getByLabel("R-free minimum").fill("0.3");
  await panel.getByLabel("R-free maximum").fill("0.2");
  await panel.getByRole("button", { name: "Apply filters" }).click();

  // then the panel says so and does not search
  await expect(panel.getByText("The minimum is above the maximum.", { exact: true })).toBeVisible();
  await expect(page).toHaveURL("/browse");

  // when a hand-edited URL holds a value the API refuses
  await page.goto("/browse?model_type=Ensemble&resolution_max=abc");

  // then the page shows the API's reason, which names the parameter
  const notice = page.getByRole("alert").filter({ hasText: "This search could not run" });
  await expect(notice).toContainText("resolution_max");
  await expect(notice.getByRole("link", { name: "Clear filters" })).toHaveAttribute("href", "/browse");
});
