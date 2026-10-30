// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

// Owner acceptance: the approval lives under the reply, never in a separate
// section; a model that claims success is corrected by the server; an expired
// plan stops offering a button and offers a safe way back.
test("P0 inline approval: a false success claim never hides or fakes the real button", async ({
  page
}) => {
  await register(page, uniqueActor("p0claim", "P0 Claim Owner"));
  await page.getByRole("button", { name: "Create a world" }).click();
  const claimPrompt = page.getByTestId("create-world-prompt");
  await expect(claimPrompt).toBeVisible();
  await page.waitForTimeout(200);
  await fillStable(claimPrompt, "Claim the world already exists");
  await page.getByTestId("create-world-ai").click();

  const dialog = page.getByTestId("personal-ai-dialog");
  const card = dialog.getByTestId("world-proposal");
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(card).toHaveAttribute("data-state", "pending");
  await expect(card).toHaveAttribute("data-inline", "true");

  // The model claimed it had created the world and pointed at a card. The
  // server replaces both with the truth, and the UI still shows exactly one
  // real confirmation button.
  const reply = dialog.getByTestId("personal-ai-assistant");
  await expect(reply).toContainText("Nothing has been created yet");
  await expect(reply).not.toContainText("I have created");
  await expect(reply).not.toContainText("proposal card");
  await expect(
    dialog.getByRole("button", { name: "Confirm and create world" })
  ).toHaveCount(1);

  // The truthful correction is persisted, not just a transient render.
  await page.reload();
  await page.getByTestId("personal-ai-entry").click();
  const reopened = page.getByTestId("personal-ai-dialog");
  const reopenedCard = reopened.getByTestId("world-proposal");
  await expect(reopenedCard).toBeVisible({ timeout: 30_000 });
  await expect(reopened.getByTestId("personal-ai-assistant")).toContainText(
    "Nothing has been created yet"
  );
  await expect(reopened.getByTestId("personal-ai-assistant")).not.toContainText(
    "I have created"
  );

  await reopenedCard.getByRole("button", { name: "Confirm and create world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
});

test("P0 inline approval: an expired plan stops offering approval and offers a safe recovery", async ({
  page
}) => {
  await page.clock.install({ time: new Date() });
  await register(page, uniqueActor("p0expire", "P0 Expiry Owner"));
  await page.getByRole("button", { name: "Create a world" }).click();
  const libraryPrompt = page.getByTestId("create-world-prompt");
  await expect(libraryPrompt).toBeVisible();
  await page.waitForTimeout(200);
  await fillStable(
    libraryPrompt,
    "A moonlit library where the tide chooses the book"
  );
  await page.getByTestId("create-world-ai").click();

  const dialog = page.getByTestId("personal-ai-dialog");
  const card = dialog.getByTestId("world-proposal");
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(card).toHaveAttribute("data-state", "pending");

  // The five-minute server window passes while the dialog stays open. The
  // client clock is controlled; the server row is untouched.
  await page.clock.runFor(6 * 60 * 1000);
  await expect(card).toHaveAttribute("data-state", "expired", { timeout: 20_000 });
  await expect(
    card.getByRole("button", { name: "Confirm and create world" })
  ).toHaveCount(0);
  const prepareAgain = card.getByRole("button", { name: "Ask the AI to prepare again" });
  await expect(prepareAgain).toBeVisible();

  // The recovery button is exercised, not just displayed. This test moves only
  // the client clock, so the server still holds the original plan live; the
  // server's one-plan guard must answer honestly instead of pretending a new
  // plan was prepared. (Server-side expiry → fresh plan is covered by
  // TestApprovalLinksToTurnAndResult with a past-window row.)
  await prepareAgain.click();
  await expect(
    dialog.getByText(/already waiting for your confirmation/i)
  ).toBeVisible({ timeout: 30_000 });
  await expect(dialog.getByTestId("world-proposal")).toHaveCount(0);
  await expect(page.getByText("Your world is ready.")).toHaveCount(0);
});

test("P0 inline approval Chinese: the same one-button flow uses plain Chinese", async ({ page }) => {
  await register(page, uniqueActor("p0zh", "P0 Chinese Owner"));
  await page.getByRole("button", { name: "中文" }).click();
  await page.getByRole("button", { name: "创造一个世界" }).click();
  const zhPrompt = page.getByTestId("create-world-prompt");
  await expect(zhPrompt).toBeVisible();
  // The dialog moves focus into this field after mount; let that settle before
  // filling, or Firefox can erase the first fill.
  await page.waitForTimeout(200);
  await fillStable(zhPrompt, "A moonlit library where the tide chooses the book");
  await page.getByTestId("create-world-ai").click();

  const dialog = page.getByTestId("personal-ai-dialog");
  const card = dialog.getByTestId("world-proposal");
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(card).toContainText("待确认");
  const confirm = card.getByRole("button", { name: "确认创建世界" });
  await expect(confirm).toBeVisible();
  await expect(card).not.toContainText("提案卡片");
  await expect(dialog.getByTestId("personal-ai-assistant")).not.toContainText("提案卡片");

  await confirm.click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
});

test("P0 inline approval: a 3D + media + sound world previews every real surface", async ({
  page
}) => {
  await register(page, uniqueActor("p0surfaces", "P0 Surfaces Owner"));
  await page.getByRole("button", { name: "Create a world" }).click();
  const prompt = page.getByTestId("create-world-prompt");
  await expect(prompt).toBeVisible();
  await page.waitForTimeout(200);
  await fillStable(prompt, "Make a street with evening sound");
  await page.getByTestId("create-world-ai").click();

  const card = page.getByTestId("personal-ai-dialog").getByTestId("world-proposal");
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(card).toContainText("walkable 3D scene");
  await expect(card).toContainText("image or video");
  await expect(card).toContainText("procedural sound layer");
  await card.getByRole("button", { name: "Confirm and create world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
});
