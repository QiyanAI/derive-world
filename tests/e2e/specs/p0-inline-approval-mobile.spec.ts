// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import {
  expectNoHorizontalOverflow,
  fillStable,
  register,
  uniqueActor
} from "../support/actors";

// Requirement 14 on a real phone viewport: the plan and its one button sit
// inside the transcript, above the composer, with no horizontal overflow, and
// a tap creates the real world.
test("P0 inline approval mobile: the plan and one button fit a phone and a tap creates the world", async ({
  page
}) => {
  await register(page, uniqueActor("p0mobile", "P0 Mobile Owner"));
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
  await expect(card).toHaveAttribute("data-inline", "true");
  await expect(dialog.getByTestId("ai-turn").filter({ has: page.getByTestId("world-proposal") })).toHaveCount(1);
  await expectNoHorizontalOverflow(page);

  const confirm = card.getByRole("button", { name: "Confirm and create world" });
  await expect(confirm).toBeVisible();
  const viewport = page.viewportSize();
  expect(viewport).not.toBeNull();
  const transcript = dialog.getByTestId("personal-ai-messages");
  const composer = dialog.locator("#dw-personal-ai-draft");
  // The reveal is scheduled after a paint; poll until the real button is
  // inside the transcript's visible rect and above the composer, not merely
  // somewhere above the bottom of the viewport.
  await expect(async () => {
    const [box, transcriptBox, composerBox] = await Promise.all([
      confirm.boundingBox(),
      transcript.boundingBox(),
      composer.boundingBox()
    ]);
    expect(box).not.toBeNull();
    expect(transcriptBox).not.toBeNull();
    expect(composerBox).not.toBeNull();
    expect(box!.y).toBeGreaterThanOrEqual(transcriptBox!.y - 1);
    expect(box!.y + box!.height).toBeLessThanOrEqual(
      transcriptBox!.y + transcriptBox!.height + 1
    );
    expect(box!.y + box!.height).toBeLessThanOrEqual(composerBox!.y + 1);
    expect(box!.y + box!.height).toBeLessThanOrEqual((viewport?.height ?? 0) + 1);
  }).toPass({ timeout: 5_000 });

  await confirm.tap();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByText("Your world is ready.")).toBeVisible();
});
