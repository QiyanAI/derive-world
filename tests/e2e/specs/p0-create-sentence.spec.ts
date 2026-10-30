// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Locator, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

async function expectInside(locator: Locator, scroller: Locator): Promise<void> {
  // Reveal/focus are scheduled after a paint; poll until the button is really
  // inside the transcript's client rectangle.
  await expect(async () => {
    const [box, area] = await Promise.all([locator.boundingBox(), scroller.boundingBox()]);
    expect(box).not.toBeNull();
    expect(area).not.toBeNull();
    expect(box!.y).toBeGreaterThanOrEqual(area!.y - 1);
    expect(box!.y + box!.height).toBeLessThanOrEqual(area!.y + area!.height + 1);
  }).toPass({ timeout: 5_000 });
}

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
  await expect(card).toHaveAttribute("data-inline", "true");
  await expect(card).toContainText("Tide Lit Library");
  await expect(card).toContainText("readable interactive story");
  await expect(card).toContainText("The green book");
  await expect(card).toContainText("nothing is written until you confirm");
  await expect(dialog).not.toContainText('"proposed_actions"');
  // The approval is inside the assistant turn, not a separate bottom section.
  const assistantTurn = dialog.getByTestId("ai-turn").filter({ has: page.getByTestId("world-proposal") });
  await expect(assistantTurn).toHaveCount(1);
  await expect(dialog.locator(".dw-personal-ai-proposals")).toHaveCount(0);

  // A plan is not a creation: the server still has zero worlds.
  expect(await stageCount(page)).toBe(0);

  // Put a later turn with one very long message after the plan, so the content
  // below the pending card is taller than the transcript scrollport without
  // spending several model turns. Reopening must still reveal the plan's real
  // button, not just jump to the newest message.
  const tallPadding = `Padding message ${"x".repeat(1800)}`;
  await fillStable(dialog.locator("#dw-personal-ai-draft"), tallPadding);
  const sendButton = dialog.getByRole("button", { name: "Send", exact: true });
  await expect(sendButton).toBeEnabled();
  const turnsBefore = await dialog.getByTestId("ai-turn").count();
  await sendButton.click();
  await expect(dialog.getByTestId("ai-turn")).toHaveCount(turnsBefore + 2, { timeout: 30_000 });
  await expect(dialog.getByText(/no scripted response/).last()).toBeVisible();

  // Close and reopen: the same real plan is still under its own reply.
  await page.reload();
  await page.getByTestId("personal-ai-entry").click();
  const reopened = page.getByTestId("personal-ai-dialog");
  const reopenedCard = reopened.getByTestId("world-proposal");
  await expect(reopenedCard).toBeVisible({ timeout: 30_000 });
  await expect(reopenedCard).toHaveAttribute("data-inline", "true");
  const transcript = reopened.getByTestId("personal-ai-messages");
  const transcriptMetrics = await transcript.evaluate((node) => ({
    scrollHeight: node.scrollHeight,
    clientHeight: node.clientHeight
  }));
  // The scenario only discriminates the scroll fix when the content below the
  // pending card is taller than the scrollport.
  expect(transcriptMetrics.scrollHeight).toBeGreaterThan(transcriptMetrics.clientHeight + 200);
  const revealButton = reopenedCard.getByRole("button", { name: "Confirm and create world" });
  await expectInside(revealButton, transcript);
  expect(await stageCount(page)).toBe(0);

  // Saying "approve" in text never creates a second request and never spends
  // another model turn; it points at the real button instead.
  const userMessagesBefore = await reopened.getByTestId("personal-ai-user").count();
  await fillStable(reopened.locator("#dw-personal-ai-draft"), "批准通过");
  await reopened.getByRole("button", { name: "Send", exact: true }).click();
  await expect(reopened.getByText(/directly under your reply/i)).toBeVisible();
  await expect(reopened.getByTestId("personal-ai-user")).toHaveCount(userMessagesBefore);
  await expect(reopenedCard).toHaveAttribute("data-state", "pending");
  const focusedButton = reopenedCard.getByRole("button", { name: "Confirm and create world" });
  await expect(focusedButton).toBeFocused();
  await expectInside(focusedButton, transcript);

  await reopenedCard.getByRole("button", { name: "Confirm and create world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByText("Your world is ready.")).toBeVisible();
  await expect(page.getByTestId("world-story")).toBeVisible({ timeout: 30_000 });
  const worldURL = page.url();

  // Once the world exists, reopening the conversation offers the real entry
  // and never asks for the same approval again.
  await page.goto("/");
  await page.getByTestId("personal-ai-entry").click();
  const createdCard = page.getByTestId("personal-ai-dialog").getByTestId("world-proposal");
  await expect(createdCard).toHaveAttribute("data-state", "created", { timeout: 30_000 });
  const createdEntry = createdCard.getByRole("link", { name: "Enter world" });
  await expect(createdEntry).toBeVisible();
  const worldID = worldURL.split("/worlds/")[1].split(/[?#]/)[0];
  await expect(createdEntry).toHaveAttribute("href", `/worlds/${worldID}`);
  await expect(
    page.getByTestId("personal-ai-dialog").getByRole("button", { name: "Confirm and create world" })
  ).toHaveCount(0);
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();

  await page.goto(worldURL);
  await expect(page.getByTestId("world-story")).toBeVisible({ timeout: 30_000 });
  await page.reload();
  await expect(page.getByTestId("world-story")).toBeVisible({ timeout: 30_000 });
});
