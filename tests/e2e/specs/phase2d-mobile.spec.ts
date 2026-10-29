// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

async function createViaAI(page: Page, prompt: string, surface: string): Promise<void> {
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).first().click();
  await fillStable(page.getByLabel("What do you want to begin?"), prompt);
  await page.getByTestId("create-world-ai").click();
  const proposal = page.getByTestId("personal-ai-dialog").getByTestId("world-proposal").first();
  await expect(proposal).toBeVisible({ timeout: 30_000 });
  await proposal.getByRole("button", { name: "Create this world" }).click();
  await expect(page.getByTestId(surface)).toBeVisible({ timeout: 30_000 });
}

test("Phase 2D mobile: custom, story and workspace surfaces fit the viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await register(page, uniqueActor("p2dmobile", "Phase 2D Mobile"));

  await createViaAI(page, "Create a world that is a playable star instrument I can touch",
    "world-custom");
  const customBox = await page.getByTestId("world-custom").boundingBox();
  expect(customBox?.width ?? 0).toBeLessThanOrEqual(390);
  await page.getByTestId("custom-node-pad_a").click({ force: true });
  await expect
    .poll(async () => page.getByTestId("custom-node-readout").locator("text").textContent(),
      { timeout: 15_000 })
    .toBe("1");

  await createViaAI(page,
    "Create a world that is an interactive story about a tide library where the reader chooses which door opens",
    "world-story");
  const storyBox = await page.getByTestId("world-story").boundingBox();
  expect(storyBox?.width ?? 0).toBeLessThanOrEqual(390);
  await page.getByTestId("story-choice").first().click();
  await expect(page.getByTestId("story-chapter")).toBeVisible();

  await createViaAI(page,
    "Create a world that is a workbench for my writing plan with editable rows and a total",
    "world-workspace");
  const workBox = await page.getByTestId("world-workspace").boundingBox();
  expect(workBox?.width ?? 0).toBeLessThanOrEqual(390);
  await page.getByTestId("workspace-global-action").first().click();
  await expect.poll(async () => page.getByTestId("workspace-row").count(), { timeout: 15_000 })
    .toBeGreaterThan(0);
});
