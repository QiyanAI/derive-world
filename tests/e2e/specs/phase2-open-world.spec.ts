// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { register, uniqueActor, fillStable } from "../support/actors";

async function createFreeWorld(page: Page, title: string): Promise<string> {
  await page.goto("/");
  await page.getByRole("button", { name: "Create a beginning" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), title);
  await page.getByRole("button", { name: "Define a free world" }).click();
  await page.getByRole("button", { name: "Let it grow" }).click();
  await expect(page.getByTestId("open-world-page")).toBeVisible();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/);
  await page.getByRole("button", { name: "Structure", exact: true }).click();
  return page.url().split("/worlds/")[1].split("?")[0];
}

test("Phase 2 free world: define kinds, place things, run a rule, persist and derive", async ({
  page
}) => {
  const actor = uniqueActor("p2world", "Phase Two World Owner");
  await register(page, actor);
  const worldTitle = "A world made of tides";
  const worldID = await createFreeWorld(page, worldTitle);
  await expect(page.getByTestId("open-world-page")).toContainText("0 kinds · 0 things · 0 rules");

  const structure = page.getByTestId("open-world-structure");

  // Define a kind that does not exist in any fixed taxonomy.
  await structure.locator("summary").filter({ hasText: "Add a kind" }).click();
  await page.locator("#ow-kind-name").fill("Tide");
  const addKind = structure.locator("details.dw-open-add").filter({ hasText: "Add a kind" }).first();
  await addKind.getByPlaceholder("Key").fill("level");
  await addKind.getByPlaceholder("Label").fill("Level");
  await addKind.locator("select").last().selectOption("number");
  await addKind.getByRole("button", { name: "Add field" }).click();
  await addKind.locator("form").getByRole("button", { name: "Add a kind", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").first()).toContainText("Tide");
  await expect(structure.locator(".dw-open-structure-list").first()).toContainText("Level");

  // Place two things of that kind.
  await structure.locator("summary").filter({ hasText: "Place a thing" }).click();
  await page.locator("#ow-node-name").fill("Morning tide");
  await page.locator("#ow-node-text").fill("The first water of the day.");
  await page.locator("form").filter({ has: page.locator("#ow-node-name") }).getByRole("button", { name: "Place a thing", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").nth(1)).toContainText("Morning tide");

  await page.locator("#ow-node-name").fill("Evening tide");
  await page.locator("#ow-node-text").fill("The water that answers the day.");
  await page.locator("form").filter({ has: page.locator("#ow-node-name") }).getByRole("button", { name: "Place a thing", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").nth(1)).toContainText("Evening tide");

  // A declarative rule: touching the morning tide sets the evening tide's level.
  await structure.locator("summary").filter({ hasText: "Add a rule" }).click();
  await page.locator("#ow-rule-trigger").selectOption({ label: "Morning tide" });
  await page.locator("#ow-effect-target").selectOption({ label: "Evening tide" });
  await page.locator("#ow-effect-field").selectOption("level");
  await page.locator("#ow-effect-value").fill("3");
  await page.locator("form").filter({ has: page.locator("#ow-rule-trigger") }).getByRole("button", { name: "Add a rule", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").nth(3)).toContainText("Morning tide");

  // Connect the two things and confirm the relation is real state.
  await structure.locator("summary").filter({ hasText: "Connect two things" }).click();
  await page.locator("#ow-link-from").selectOption({ label: "Morning tide" });
  await page.locator("#ow-link-to").selectOption({ label: "Evening tide" });
  await page.locator("#ow-link-label").fill("becomes");
  await page.locator("form").filter({ has: page.locator("#ow-link-from") }).getByRole("button", { name: "Connect two things", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").nth(2)).toContainText("becomes");

  // Cards expose real values. Touching the trigger runs the rule once.
  await page.getByRole("button", { name: "Cards", exact: true }).click();
  const morning = page
    .getByTestId("open-world-card")
    .filter({ hasText: "Morning tide" });
  const evening = page
    .getByTestId("open-world-card")
    .filter({ hasText: "Evening tide" });
  await expect(morning).toBeVisible();
  await expect(evening).toBeVisible();
  await morning.getByRole("button", { name: "Touch" }).click();
  await expect(evening.locator('input[type="number"]')).toHaveValue("3");

  // Reload to prove the change was persisted, not just rendered.
  await page.reload();
  await page.getByRole("button", { name: "Cards", exact: true }).click();
  const eveningAfterReload = page
    .getByTestId("open-world-card")
    .filter({ hasText: "Evening tide" });
  await expect(eveningAfterReload.locator('input[type="number"]')).toHaveValue("3");

  // The world's source structure and current state are readable through the
  // real object endpoint.
  const parent = await page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/objects/${id}`, { credentials: "include" });
    return (await response.json()) as {
      object_type: string;
      state: {
        kinds: Array<{ id: string; name: string }>;
        nodes: Array<{ name: string }>;
        links: unknown[];
        rules: unknown[];
      };
    };
  }, worldID);
  expect(parent.object_type).toBe("open_world");
  expect(parent.state.kinds.map((kind) => kind.name)).toEqual(["Tide"]);
  expect(parent.state.nodes.map((node) => node.name)).toEqual(["Morning tide", "Evening tide"]);
  expect(parent.state.links).toHaveLength(1);
  expect(parent.state.rules).toHaveLength(1);
  const parentKindIDs = parent.state.kinds.map((kind) => kind.id);

  // Derive the world with its structure; the child keeps the shape but gets
  // new identity and does not rewrite the parent.
  await page.getByRole("link", { name: "Branch", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/objects/${worldID}/derive`));
  await page.getByLabel("Title of the new branch").fill("A world of different tides");
  await page.getByRole("button", { name: "Create derived object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}\?derived=1/);
  const childID = page.url().split("/objects/")[1].split("?")[0];
  expect(childID).not.toBe(worldID);
  const child = await page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/objects/${id}`, { credentials: "include" });
    return (await response.json()) as {
      state: {
        mention?: string;
        kinds: Array<{ id: string; name: string }>;
        nodes: Array<{ name: string }>;
        links: unknown[];
        rules: unknown[];
      };
    };
  }, childID);
  expect(child.state.nodes.map((node) => node.name)).toEqual(["Morning tide", "Evening tide"]);
  expect(child.state.links).toHaveLength(1);
  expect(child.state.rules).toHaveLength(1);
  for (const kind of child.state.kinds) {
    expect(parentKindIDs).not.toContain(kind.id);
  }
});
