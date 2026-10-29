// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

async function createViaAI(page: Page, prompt: string, surface: string): Promise<string> {
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).first().click();
  await fillStable(page.getByLabel("What do you want to begin?"), prompt);
  await page.getByTestId("create-world-ai").click();
  const dialog = page.getByTestId("personal-ai-dialog");
  await expect(dialog).toBeVisible();
  const proposal = dialog.getByTestId("world-proposal").first();
  await expect(proposal).toBeVisible({ timeout: 30_000 });
  await expect(dialog).toContainText("Fixture");
  await proposal.getByRole("button", { name: "Create this world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByTestId(surface)).toBeVisible({ timeout: 30_000 });
  return page.url();
}

test("Phase 2D: a sentence becomes a playable game, a readable story and a workbench", async ({
  page
}) => {
  await register(page, uniqueActor("p2dforms", "Phase 2D Forms"));
  const gameURL = await createViaAI(
    page,
    "Create a world that is a playable star-dust forest where I gather three fallen lights",
    "world-live"
  );
  await expect(page.getByTestId("world-live-goal")).toContainText("Gather the three lights");
  const entities = await page.getByTestId("world-live-entity").evaluateAll((items) =>
    items.map((item) => ({
      id: item.getAttribute("data-entity-id"),
      x: Number(item.getAttribute("data-x")),
      y: Number(item.getAttribute("data-y"))
    }))
  );
  const light = entities
    .filter((entity) => entity.id)
    .sort((left, right) =>
      Math.abs(left.x - 886) + Math.abs(left.y - 500) -
      (Math.abs(right.x - 886) + Math.abs(right.y - 500)))[0];
  expect(light?.id).toBeTruthy();
  await page.locator(`[data-entity-id="${light!.id}"]`).dispatchEvent("click");
  if (!(await page.getByTestId("world-live-goal").innerText()).includes("1 / 3")) {
    // Firefox can drop a synthetic click on an animated SVG group; the same
    // in-world interaction is reachable through the proximity key.
    await page.getByTestId("world-live").focus();
    await page.keyboard.press("e");
  }
  await expect(page.getByTestId("world-live-goal")).toContainText("1 / 3", { timeout: 15_000 });

  await createViaAI(
    page,
    "Create a world that is an interactive story about a tide library where the reader chooses which door opens",
    "world-story"
  );
  const firstChapter = await page.getByTestId("story-chapter").locator("h3").innerText();
  await page.getByTestId("story-choice").first().click();
  await expect
    .poll(async () => page.getByTestId("story-chapter").locator("h3").innerText(), { timeout: 15_000 })
    .not.toBe(firstChapter);
  await page.reload();
  await expect(page.getByTestId("world-story")).toBeVisible();
  expect(await page.getByTestId("story-chapter").locator("h3").innerText()).not.toBe(firstChapter);

  await createViaAI(
    page,
    "Create a world that is a workbench for my writing plan with editable rows and a total",
    "world-workspace"
  );
  const rowsBefore = await page.getByTestId("workspace-row").count();
  expect(rowsBefore).toBeGreaterThan(0);
  await page.getByTestId("workspace-global-action").first().click();
  await expect
    .poll(async () => page.getByTestId("workspace-row").count(), { timeout: 15_000 })
    .toBe(rowsBefore + 1);
  await page.reload();
  await expect(page.getByTestId("world-workspace")).toBeVisible();
  expect(await page.getByTestId("workspace-row").count()).toBe(rowsBefore + 1);
  expect(gameURL).toContain("/worlds/");
});

test("Phase 2D: animation plays, a custom surface persists, and reshape/derive stay independent", async ({
  page
}) => {
  await register(page, uniqueActor("p2dreshape", "Phase 2D Reshape"));
  await createViaAI(
    page,
    "Create a world that is an animated nebula that breathes and drifts",
    "world-live"
  );
  const controls = page.getByTestId("world-live-controls");
  await expect(controls).toBeVisible();
  const before = await controls.locator("small").innerText();
  await expect
    .poll(async () => controls.locator("small").innerText(), { timeout: 15_000 })
    .not.toBe(before);

  const customURL = await createViaAI(
    page,
    "Create a world that is a playable star instrument I can touch",
    "world-custom"
  );
  await page.getByTestId("custom-node-pad_a").click({ force: true });
  await expect
    .poll(async () => page.getByTestId("custom-node-readout").locator("text").textContent(),
      { timeout: 15_000 })
    .toBe("1");
  await page.reload();
  await expect(page.getByTestId("world-custom")).toBeVisible();
  expect(await page.getByTestId("custom-node-readout").locator("text").textContent()).toBe("1");

  // Natural-language reshape on a live game.
  const gameURL = await createViaAI(
    page,
    "Create a world that is a playable star-dust forest where I gather three fallen lights",
    "world-live"
  );
  await page.getByTestId("ai-entry").click();
  await fillStable(page.locator("#dw-ai-compose"), "Turn this world into a playable star instrument");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  const proposal = page.getByTestId("proposal-card").first();
  await expect(proposal).toBeVisible({ timeout: 30_000 });
  await proposal.getByRole("button", { name: "Apply proposal" }).click();
  await expect(page.getByTestId("world-custom")).toBeVisible({ timeout: 30_000 });
  await page.getByTestId("custom-node-pad_a").click({ force: true });
  await expect
    .poll(async () => page.getByTestId("custom-node-readout").locator("text").textContent(),
      { timeout: 15_000 })
    .toBe("1");

  // Branch the reshaped world and change only the child.
  await page.getByText("Advanced", { exact: true }).click();
  await page.getByRole("button", { name: "Derive a new world" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Create this branch" }).click();
  await expect(page).not.toHaveURL(gameURL, { timeout: 30_000 });
  await expect(page.getByTestId("world-custom")).toBeVisible({ timeout: 30_000 });
  const childURL = page.url();
  expect(childURL).not.toBe(gameURL);
  await page.getByTestId("custom-node-pad_a").click({ force: true });
  await expect
    .poll(async () => page.getByTestId("custom-node-readout").locator("text").textContent(),
      { timeout: 15_000 })
    .toBe("2");
  await page.goto(gameURL);
  await expect(page.getByTestId("world-custom")).toBeVisible({ timeout: 30_000 });
  expect(await page.getByTestId("custom-node-readout").locator("text").textContent()).toBe("1");
  expect(customURL).toContain("/worlds/");
});

test("Phase 2D: one world can be played and read at the same time", async ({ page }) => {
  await register(page, uniqueActor("p2dcombined", "Phase 2D Combined"));
  await createViaAI(
    page,
    "Create a world that is a playable adventure with a story I can read",
    "world-experience-tabs"
  );
  const tabs = page.getByTestId("world-experience-tabs");
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  await expect(page.getByTestId("world-live")).toBeVisible();
  await expect(page.getByTestId("world-live-goal")).toContainText("1");
  await tabs.getByRole("tab").nth(1).click();
  await expect(page.getByTestId("world-story")).toBeVisible();
  const first = await page.getByTestId("story-chapter").locator("h3").innerText();
  await page.getByTestId("story-choice").first().click();
  await expect
    .poll(async () => page.getByTestId("story-chapter").locator("h3").innerText(), { timeout: 15_000 })
    .not.toBe(first);
});
