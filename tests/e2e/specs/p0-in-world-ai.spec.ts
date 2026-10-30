// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Locator, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

async function expectInside(locator: Locator, scroller: Locator): Promise<void> {
  await expect(async () => {
    const [box, area] = await Promise.all([locator.boundingBox(), scroller.boundingBox()]);
    expect(box).not.toBeNull();
    expect(area).not.toBeNull();
    expect(box!.y).toBeGreaterThanOrEqual(area!.y - 1);
    expect(box!.y + box!.height).toBeLessThanOrEqual(area!.y + area!.height + 1);
  }).toPass({ timeout: 5_000 });
}

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
    const compose = page.locator("#dw-ai-compose");
    const sendButton = page.getByRole("button", { name: "Send", exact: true });
    await fillStable(compose, text);
    await expect(sendButton).toBeEnabled();
    const turnsBefore = await page.getByTestId("ai-turn").count();
    // A single click must start the turn. There is no retry and no pre-click
    // sleep, so a swallowed click fails the suite instead of being masked.
    await sendButton.click();
    await expect(compose).toHaveValue("", { timeout: 30_000 });
    // Wait for the assistant turn so the next send never races a busy panel.
    await expect(page.getByTestId("ai-turn")).toHaveCount(turnsBefore + 2, { timeout: 30_000 });
  }

  await openAI();
  await expect(page.getByTestId("ai-world-context")).toContainText("A blank room for the AI");

  // Scenario 2: a completely blank world is bootstrapped by a real proposal.
  await send("Start this world from nothing");
  const bootstrap = page
    .getByTestId("proposal-card")
    .filter({ hasText: "A walkable clearing with three lights." });
  await expect(bootstrap).toBeVisible({ timeout: 30_000 });
  // The approval is inside the transcript, under the assistant turn. A
  // regression that moved it back to a separate bottom section would fail
  // both assertions.
  await expect(page.locator(".ai-messages [data-testid='proposal-card']")).toHaveCount(1);
  await expect(
    page.getByTestId("ai-turn").filter({ has: page.getByTestId("proposal-card") })
  ).toHaveCount(1);
  let snapshot = await worldSnapshot(page, objectID);
  expect(snapshot.state.nodes).toHaveLength(0);
  await bootstrap.getByRole("button", { name: "Confirm and apply" }).click();
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

  // Add later turns, one with a very long message, so the pending proposal is
  // no longer the last message and the content below it is taller than the
  // 360px scrollport without spending several model turns.
  await send("Just say hello to this world");
  await expect(
    page.getByText("Hello, world. Nothing needs to change right now.")
  ).toBeVisible({ timeout: 30_000 });
  await send(`Padding message ${"x".repeat(1800)}`);
  await expect(page.getByText(/no scripted response/).last()).toBeVisible({ timeout: 30_000 });

  // Closing the world and reopening it must find the same pending proposal
  // under the same assistant reply, not a jump to the newest message.
  await page.reload();
  await expect(page.getByTestId("open-world-page")).toBeVisible({ timeout: 30_000 });
  await openAI();
  const reopenedProposal = page
    .getByTestId("proposal-card")
    .filter({ hasText: "A quiet rumor moves through the empty rooms." });
  await expect(reopenedProposal).toBeVisible({ timeout: 30_000 });
  await expect(reopenedProposal).toHaveAttribute("data-inline", "true");
  await expect(reopenedProposal).toHaveAttribute("data-state", "pending");
  const applyButton = reopenedProposal.getByRole("button", { name: "Confirm and apply" });
  const inWorldTranscript = page.locator(".ai-messages");
  const inWorldMetrics = await inWorldTranscript.evaluate((node) => ({
    scrollHeight: node.scrollHeight,
    clientHeight: node.clientHeight
  }));
  // The scenario only discriminates the scroll fix when the content below the
  // pending card is taller than the transcript scrollport.
  expect(inWorldMetrics.scrollHeight).toBeGreaterThan(inWorldMetrics.clientHeight + 200);
  await expectInside(applyButton, inWorldTranscript);

  // Saying "批准通过" focuses the real inline button and spends no new turn.
  const turnsBeforeApproval = await page.getByTestId("ai-turn").count();
  const approvalCompose = page.locator("#dw-ai-compose");
  await fillStable(approvalCompose, "批准通过");
  const approvalSend = page.getByRole("button", { name: "Send", exact: true });
  await expect(approvalSend).toBeEnabled();
  await approvalSend.click();
  await expect(
    page.getByText("The plan is directly under your reply. Press the confirm button there.")
  ).toBeVisible();
  await expect(page.getByTestId("ai-turn")).toHaveCount(turnsBeforeApproval);
  await expect(applyButton).toBeFocused();
  await expectInside(applyButton, inWorldTranscript);

  await applyButton.click();
  await expect(page.getByText("Proposal applied.")).toBeVisible({ timeout: 30_000 });
  const premise = page.locator(".dw-open-premise-card");
  await expect(premise).toContainText("A quiet rumor moves through the empty rooms.");
  await page.reload();
  await expect(premise).toContainText("A quiet rumor moves through the empty rooms.");

  // Scenario 3b: a visual/interaction change is applied to the same world.
  await send("Turn this world into a playable star instrument");
  const visualProposal = page
    .getByTestId("proposal-card")
    .filter({ hasText: "Turn the world into a playable star instrument" });
  await expect(visualProposal).toBeVisible({ timeout: 30_000 });
  await visualProposal.getByRole("button", { name: "Confirm and apply" }).click();
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
  // The confirmation must still name the source version it is bound to.
  await expect(confirmation).toContainText(`Bound to v${afterChanges.version}`);
  await confirmation.getByRole("button", { name: "Confirm and create branch" }).click();
  await expect(confirmation).toHaveAttribute("data-state", "created", { timeout: 30_000 });
  const parentAfterDerive = await worldSnapshot(page, objectID);
  expect(parentAfterDerive.version).toBe(afterChanges.version);
  await confirmation.getByRole("link", { name: "Enter the new world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/);
  await expect(page.getByRole("heading", { name: "Tide Library, Another Tide", level: 1 })).toBeVisible();
});
