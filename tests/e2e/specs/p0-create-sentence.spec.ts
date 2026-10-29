// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

async function stageCount(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const response = await fetch("/api/v1/world/stage", { credentials: "include" });
    const data = (await response.json()) as { objects: unknown[] };
    return data.objects.length;
  });
}

// Owner acceptance scenario 1: a brand-new user writes one sentence, sees a
// human-readable plan, approves it, and enters the real world. The sentence
// deliberately avoids the fixture model's built-in keywords, so the test
// proves the scripted reply (not a keyword template) drives the flow.
test("P0 creation: one sentence becomes a previewed world only after approval", async ({ page }) => {
  test.setTimeout(180_000);
  await register(page, uniqueActor("p0create", "P0 Creator"));
  await expect(page.getByTestId("world-onboarding")).toBeVisible();
  expect(await stageCount(page)).toBe(0);

  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(
    page.getByTestId("create-world-prompt"),
    "A moonlit library where the tide chooses the book"
  );

  // The default creation path asks one question and offers no template,
  // type, select or technical field.
  const form = page.locator("#dw-create-world-form");
  await expect(form.locator('input[type="radio"]')).toHaveCount(0);
  await expect(form.locator("select")).toHaveCount(0);
  await expect(page.getByRole("dialog").getByRole("button", { name: "Create with my AI" })).toBeVisible();

  await page.getByTestId("create-world-ai").click();
  const dialog = page.getByTestId("personal-ai-dialog");
  const card = dialog.getByTestId("world-proposal");
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(card).toContainText("Tide Lit Library");
  await expect(card).toContainText("readable interactive story");
  await expect(card).toContainText("The green book");
  await expect(card).toContainText("nothing is written until you confirm");
  await expect(dialog).not.toContainText('"proposed_actions"');

  // A plan is not a creation: the server still has zero worlds.
  expect(await stageCount(page)).toBe(0);

  await card.getByRole("button", { name: "Create this world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByText("Your world is ready.")).toBeVisible();
  await expect(page.getByTestId("world-story")).toBeVisible({ timeout: 30_000 });

  await page.reload();
  await expect(page.getByTestId("world-story")).toBeVisible({ timeout: 30_000 });
});
