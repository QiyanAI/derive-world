// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

async function createFreeWorld(page: Page, title: string): Promise<string> {
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), title);
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  await expect(page.getByTestId("open-world-page")).toBeVisible();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/);
  return page.url().split("/worlds/")[1].split("?")[0];
}

async function addTaskKindAndNode(page: Page): Promise<void> {
  await page.getByText("Advanced", { exact: true }).click();
  await page.getByRole("button", { name: "World structure" }).click();
  const structure = page.getByTestId("open-world-structure");
  const kindDetails = structure.locator("details.dw-open-add").filter({ hasText: "Add a kind" }).first();
  await kindDetails.evaluate((element) => {
    (element as HTMLDetailsElement).open = true;
  });
  await page.locator("#ow-kind-name").fill("Task");
  const kindForm = structure.locator("form").filter({ has: page.locator("#ow-kind-name") });
  await kindForm.getByPlaceholder("Key").fill("progress");
  await kindForm.getByPlaceholder("Label").fill("Progress");
  await kindForm.locator("select").last().selectOption("number");
  await kindForm.getByRole("button", { name: "Add field" }).click();
  await kindForm.getByRole("button", { name: "Add a kind", exact: true }).click();

  const nodeDetails = structure.locator("details.dw-open-add").filter({ hasText: "Place a thing" }).first();
  await nodeDetails.evaluate((element) => {
    (element as HTMLDetailsElement).open = true;
  });
  await page.locator("#ow-node-name").fill("First task");
  await page.locator("#ow-node-text").fill("A real thing in a real world.");
  await page.locator("#ow-node-x").fill("500");
  await page.locator("#ow-node-y").fill("420");
  const nodeForm = structure.locator("form").filter({ has: page.locator("#ow-node-name") });
  await nodeForm.getByRole("button", { name: "Place a thing", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").nth(1)).toContainText("First task");
}

async function composeWorkspace(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Experience editor", exact: true }).click();
  const editor = page.getByTestId("world-experience-editor");
  await expect(editor).toBeVisible();
  await page.getByTestId("world-experience-scaffold-workspace").click();

  await page.getByRole("button", { name: "Add a behavior", exact: true }).click();
  const behavior = page.locator('[data-testid^="world-experience-behavior-"]').first();
  await expect(behavior).toBeVisible();
  const action = behavior.locator('[data-testid="world-experience-action"]').first();
  await action.getByLabel("Action", { exact: true }).selectOption("add");
  await action.getByLabel("Thing", { exact: true }).selectOption({ label: "First task" });
  await action.getByLabel("Value", { exact: true }).fill("1");

  await page.getByTestId("world-experience-node-root").click();
  await page.getByTestId("world-experience-add-core.button").click();
  await expect(page.getByTestId("world-experience-inspector")).toContainText("Action button");
  await page.getByTestId("world-experience-inspector").getByLabel("Label", { exact: true }).fill("Add one");
  await page.getByTestId("world-experience-save").click();

  const experience = page.getByTestId("world-experience");
  await expect(experience).toBeVisible();

  // The composed experience is one selectable view; the classic views remain
  // available and unmodified for the same world.
  const viewbar = page.locator(".dw-open-viewbar");
  await expect(viewbar.getByRole("button", { name: "Experience", exact: true })).toHaveClass(/is-active/);
  await viewbar.getByRole("button", { name: "Scene", exact: true }).click();
  await expect(page.getByTestId("open-world-scene")).toBeVisible();
  await viewbar.getByRole("button", { name: "Experience", exact: true }).click();
  await expect(experience).toBeVisible();

  const row = page.getByTestId("world-experience-row").filter({ hasText: "First task" });
  await expect(row).toContainText("0");
  await page.getByTestId("world-experience-button").filter({ hasText: "Add one" }).click();
  await expect.poll(async () => row.innerText(), { timeout: 15_000 }).toContain("1");
  await page.reload();
  await expect(page.getByTestId("world-experience")).toBeVisible();
  await expect(page.getByTestId("world-experience-row").filter({ hasText: "First task" })).toContainText("1");
}

test("Phase 2C: compose a capability experience, run it, persist it and derive it", async ({ page }) => {
  const actor = uniqueActor("p2cap", "Phase Two Capability Owner");
  await register(page, actor);
  const worldID = await createFreeWorld(page, "A capability-composed world");

  // The classic structure editor still owns content; the experience editor
  // composes presentation and behavior from registered capabilities.
  await addTaskKindAndNode(page);
  await composeWorkspace(page);

  await page.getByText("Advanced", { exact: true }).click();
  await page.getByRole("button", { name: "Derive a new world" }).click();
  await page.getByRole("link", { name: "Need more options? Open the full derive page" }).click();
  await page.getByLabel("Title of the new branch").fill("A capability-composed branch");
  await page.getByRole("button", { name: "Create derived object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}\?derived=1/);
  const childID = page.url().split("/objects/")[1].split("?")[0];
  expect(childID).not.toBe(worldID);

  await page.goto(`/worlds/${childID}`);
  await expect(page.getByTestId("world-experience")).toBeVisible();
  const childRow = page.getByTestId("world-experience-row").filter({ hasText: "First task" });
  await expect(childRow).toContainText("1");
  await page.getByTestId("world-experience-button").filter({ hasText: "Add one" }).click();
  await expect.poll(async () => childRow.innerText(), { timeout: 15_000 }).toContain("2");

  await page.goto(`/worlds/${worldID}`);
  await expect(page.getByTestId("world-experience-row").filter({ hasText: "First task" })).toContainText("1");
});
