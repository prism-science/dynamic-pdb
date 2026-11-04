import { signIn, submitter } from "./support/auth";
import { expect, test } from "./support/fixtures";

// website/src/app/components/entry-form/types.ts
const entryDraftStorageKey = "dpdb:new-entry-draft";

test.beforeEach(async ({ context }) => {
  await signIn(context, submitter);
});

test("should restore the typed values when the page is reloaded", async ({ page, runTag }) => {
  // given
  const title = `T2-D draft ${runTag}`;
  await page.goto("/entries/new");
  await page.getByLabel("Title", { exact: true }).fill(title);
  await page.getByLabel("Description", { exact: true }).fill("A draft kept across a reload.");
  await page.getByLabel("Method", { exact: true }).selectOption("X-ray crystallography");
  await expect
    .poll(() => page.evaluate((key) => localStorage.getItem(key) ?? "", entryDraftStorageKey))
    .toContain("A draft kept across a reload.");

  // when
  await page.reload();
  const prompt = page.getByRole("dialog");
  await expect(prompt.getByRole("heading", { name: "Continue where you left off?" })).toBeVisible();
  await expect(prompt).toContainText(title);
  await prompt.getByRole("button", { name: "Continue" }).click();

  // then
  await expect(prompt).toBeHidden();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue(title);
  await expect(page.getByLabel("Description", { exact: true })).toHaveValue(
    "A draft kept across a reload.",
  );
  await expect(page.getByLabel("Method", { exact: true })).toHaveValue("X-ray crystallography");
});
