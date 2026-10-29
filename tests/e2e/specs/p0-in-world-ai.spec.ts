// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

interface WorldSnapshot {
  version: number;
  state: { nodes: Array<{ id: string }>; premise: string };
}

async function worldSnapshot(page: Page, id: string): Promise<WorldSnapshot> {
  return page.evaluate(async (objectID) => {
    const response = await fetch(`/api/v1/objects/${objectID}`, { credentials: "include" });
    return (await response.json()) as WorldSnapshot;
  }, id);
}

// Owner acceptance scenarios 2, 3, 4, 5 and 6: one world, one AI entry,
// blank bootstrap, consecutive changes, a safe unreadable-output failure and
// an AI-proposed derivation that leaves the parent untouched.
test("P0 in-world AI: bootstrap, consecutive changes, safe failure and derive", async ({ page }) => {
  test.setTimeout(240_000);
  await register(page, uniqueActor("p0world", "P0 World Owner"));

  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "A blank room for the AI");
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  await expect(page.getByTestId("open-world-page")).toBeVisible({ timeout: 30_000 });
  const worldURL = page.url();
  const objectID = worldURL.split("/worlds/")[1].split("?")[0];

  // Exactly one in-world AI entry, and no context-free global entry.
  await expect(page.getByTestId("ai-entry")).toHaveCount(1);
  await expect(page.getByTestId("personal-ai-entry")).toHaveCount(0);

  async function openAI(): Promise<void> {
    if (await page.getByTestId("ai-panel").isVisible().catch(() => false)) {
      return;
    }
    await page.getByTestId("ai-entry").click();
    await expect(page.getByTestId("ai-panel")).toBeVisible();
  }

  async function send(text: string): Promise<void> {
    await openAI();
    await fillStable(page.locator("#dw-ai-compose"), text);
    await page.getByRole("button", { name: "Send", exact: true }).click();
  }

  await openAI();
  await expect(page.getByTestId("ai-world-context")).toContainText("A blank room for the AI");

  // Scenario 2: a completely blank world is bootstrapped by a real proposal.
  await send("Start this world from nothing");
  const bootstrap = page
    .getByTestId("proposal-card")
    .filter({ hasText: "A walkable clearing with three lights." });
  await expect(bootstrap).toBeVisible({ timeout: 30_000 });
  let snapshot = await worldSnapshot(page, objectID);
  expect(snapshot.state.nodes).toHaveLength(0);
  await bootstrap.getByRole("button", { name: "Apply proposal" }).click();
  await expect(page.getByText("Proposal applied.")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByTestId("world-live")).toBeVisible({ timeout: 30_000 });
  snapshot = await worldSnapshot(page, objectID);
  expect(snapshot.state.nodes.length).toBeGreaterThan(0);
  await page.reload();
  await expect(page.getByTestId("world-live")).toBeVisible({ timeout: 30_000 });

  // Scenario 3a: a text change is proposed, applied, and survives a refresh.
  await send("Rewrite the premise as a quiet rumor");
  const textProposal = page
    .getByTestId("proposal-card")
    .filter({ hasText: "A quiet rumor moves through the empty rooms." });
  await expect(textProposal).toBeVisible({ timeout: 30_000 });
  await textProposal.getByRole("button", { name: "Apply proposal" }).click();
  await expect(page.getByText("Proposal applied.")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("A quiet rumor moves through the empty rooms.")).toBeVisible();
  await page.reload();
  await expect(page.getByText("A quiet rumor moves through the empty rooms.")).toBeVisible();

  // Scenario 3b: a visual/interaction change is applied to the same world.
  await send("Turn this world into a playable star instrument");
  const visualProposal = page
    .getByTestId("proposal-card")
    .filter({ hasText: "Turn the world into a playable star instrument" });
  await expect(visualProposal).toBeVisible({ timeout: 30_000 });
  await visualProposal.getByRole("button", { name: "Apply proposal" }).click();
  await expect(page.getByText("Proposal applied.")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByTestId("world-custom")).toBeVisible({ timeout: 30_000 });
  const afterChanges = await worldSnapshot(page, objectID);

  // Scenario 5: unreadable provider output never becomes chat text and never
  // changes the world.
  await send("Show me an unreadable plan");
  await expect(
    page.getByText("I could not turn this response into an executable plan.")
  ).toBeVisible({ timeout: 30_000 });
  await expect(
    page.getByText("Nothing was created or changed. Try again, or describe the idea a little more concretely.")
  ).toBeVisible();
  await expect(page.getByTestId("ai-panel")).not.toContainText('"proposed_actions"');
  const afterFailure = await worldSnapshot(page, objectID);
  expect(afterFailure.version).toBe(afterChanges.version);

  // Scenario 4: an AI-proposed independent derivation. The parent is not
  // modified and the confirmation names the child and its source version.
  await send("Fork this world for me");
  const confirmation = page.getByTestId("confirmation-card").first();
  await expect(confirmation).toBeVisible({ timeout: 30_000 });
  await expect(confirmation).toContainText("Tide Library, Another Tide");
  await expect(confirmation).toContainText("Bound to v");
  await confirmation.getByRole("button", { name: "Approve and execute" }).click();
  await expect(
    page.getByText(/Approval executed: created Tide Library, Another Tide at v1\./)
  ).toBeVisible({ timeout: 30_000 });
  const parentAfterDerive = await worldSnapshot(page, objectID);
  expect(parentAfterDerive.version).toBe(afterChanges.version);
  await page.getByRole("link", { name: "Open created object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
  await expect(page.getByRole("heading", { name: "Tide Library, Another Tide", level: 1 })).toBeVisible();
  await expect(page.getByText("Version 1", { exact: true }).first()).toBeVisible();
});
