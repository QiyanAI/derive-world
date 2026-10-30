// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { register, uniqueActor, fillStable } from "../support/actors";

test("V2 mobile: bottom navigation, canvas, inspector and discovery fit the viewport", async ({
  page
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  const actor = uniqueActor("v2touch", "V2 Touch Owner");
  await register(page, actor);

  await page.goto("/");
  await expect(page.getByRole("navigation", { name: "Primary navigation" })).toBeVisible();
  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "A world that fits in one hand");
  await expect(page.getByLabel("What do you want to begin?")).toHaveValue("A world that fits in one hand");
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Idea scene" }).click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
  await expect(page.getByTestId("stage-inspector")).toBeVisible();
  await page.getByTestId("stage-inspector").getByRole("tab", { name: "Branch", exact: true }).click();
  await expect(page.getByTestId("stage-branch-composer")).toBeVisible();

  // The inspector owns vertical touch scrolling; the canvas pan layer owns
  // the rest. This is the phone half of the scroll regression.
  const inspectorTouch = await page
    .getByTestId("stage-inspector")
    .evaluate((element) => getComputedStyle(element).touchAction);
  expect(inspectorTouch).toBe("pan-y");
  const panTouch = await page
    .locator(".dw-stage-pan-layer")
    .evaluate((element) => getComputedStyle(element).touchAction);
  expect(panTouch).toBe("none");

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);

  await page.goto("/friends");
  await expect(page.getByTestId("friends-page")).toBeVisible();
  await page.locator(".dw-head-actions").getByRole("button", { name: "Find a friend" }).click();
  await expect(page.getByRole("dialog")).toContainText("Find someone to create with");
  await page.locator(".dw-modal-close").click();
  const friendsOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth
  );
  expect(friendsOverflow).toBeLessThanOrEqual(1);
});
