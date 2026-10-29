// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { expectNoHorizontalOverflow, fillStable, register, uniqueActor } from "../support/actors";

test("Phase 2E mobile: the 3D surface, sound gate and media slot fit a phone", async ({ page }) => {
  test.setTimeout(120_000);
  await register(page, uniqueActor("p2emobile", "Phase 2E Mobile"));
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).first().click();
  await fillStable(
    page.getByLabel("What do you want to begin?"),
    "Create a world that is a three-dimensional planet I can walk on and hear"
  );
  await page.getByTestId("create-world-ai").click();
  const dialog = page.getByTestId("personal-ai-dialog");
  await expect(dialog).toBeVisible();
  const proposal = dialog.getByTestId("world-proposal").first();
  await expect(proposal).toBeVisible({ timeout: 30_000 });
  await expect(dialog).toContainText("Fixture");
  await proposal.getByRole("button", { name: "Create this world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByTestId("world-3d")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByTestId("world-3d-touch")).toBeVisible();
  await expect(page.getByTestId("world-audio-enable")).toBeVisible();
  await expectNoHorizontalOverflow(page);

  const photoTab = page.getByRole("tab").filter({ hasText: /photo|图像/i }).first();
  await photoTab.click();
  await expect(page.getByTestId("world-media-empty")).toBeVisible();
  await expectNoHorizontalOverflow(page);
});
