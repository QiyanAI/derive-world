// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { register, uniqueActor } from "../support/actors";

test("V2: context-first navigation, world zoom and exact-handle friend discovery", async ({ page }) => {
  await register(page, uniqueActor("v2experience", "V2 Experience"));
  await page.goto("/objects/new?template=living_scene&purpose=project&first=1");
  await page.getByRole("button", { name: "Create object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
  await page.goto("/");
  const stage = page.getByTestId("world-stage");
  await expect(stage).toBeVisible();
  await expect(stage.getByTestId("dw-stage-viewport")).toHaveAttribute("data-zoom", "1.00");
  await expect(stage.getByTestId("dw-stage-node")).toHaveCount(1);
  await expect(stage.locator(".dw-world-lines")).toBeVisible();
  await stage.getByTestId("dw-stage-zoom-in").click();
  await expect(stage.getByTestId("dw-stage-viewport")).toHaveAttribute("data-zoom", "1.15");
  await stage.getByTestId("dw-stage-zoom-reset").click();
  await expect(stage.getByTestId("dw-stage-viewport")).toHaveAttribute("data-zoom", "1.00");
  await page.goto("/friends");
  await page.getByRole("button", { name: "Find a friend" }).click();
  await expect(page.getByPlaceholder("Friend's handle")).toBeFocused();
  await page.goto("/explore");
  await expect(page.getByTestId("explore-page")).toBeVisible();
  await page.getByRole("link", { name: /AI setup/ }).click();
  await expect(page).toHaveURL(/\/settings#ai-connection/);
  await expect(page.locator("#ai-connection")).toBeVisible();
});

test("V2: narrow-screen navigation and scene controls fit without page overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await register(page, uniqueActor("v2mobile", "V2 Mobile"));
  await page.goto("/");
  await expect(page.getByTestId("dw-stage-zoom-in")).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await page.goto("/friends");
  await expect(page.getByRole("button", { name: "Find a friend" })).toBeVisible();
  await page.goto("/explore");
  await expect(page.getByTestId("explore-page")).toBeVisible();
});
