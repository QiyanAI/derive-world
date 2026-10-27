// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { login, register, uniqueActor } from "../support/actors";

async function createFirstWorld(page: import("@playwright/test").Page, title: string) {
  await page.goto("/");
  await expect(page.getByTestId("world-onboarding")).toBeVisible();
  await page.getByRole("button", { name: "Create the first world" }).click();
  await page.getByLabel("What do you want to begin?").fill(title);
  await expect(page.getByLabel("What do you want to begin?")).toHaveValue(title);
  await page.getByRole("button", { name: "Let it grow" }).click();
  await expect(page.getByTestId("stage-inspector")).toBeVisible();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
}

test("First use: create a real world, change it in place, and derive a real branch", async ({
  page
}) => {
  const actor = uniqueActor("v2direct", "V2 Direct Owner");
  await register(page, actor);
  await createFirstWorld(page, "My first real project");

  const inspector = page.getByTestId("stage-inspector");
  await inspector.getByLabel("Add a new idea or step").fill("Run one small test with a real user");
  await inspector.getByRole("button", { name: "Add", exact: true }).click();
  await expect(inspector.getByText("Run one small test with a real user")).toBeVisible();
  await expect(inspector.getByText(/0\/2 complete/)).toBeVisible();

  await inspector.getByRole("tab", { name: "Branch", exact: true }).click();
  await expect(inspector.getByTestId("stage-branch-composer")).toBeVisible();
  await inspector.getByLabel("Branch title").fill("Direct branch");
  await inspector.getByRole("button", { name: "Preview branch" }).click();
  await expect(inspector.getByTestId("stage-branch-preview")).toContainText(
    "Run one small test with a real user"
  );
  await inspector.getByRole("button", { name: "Create this branch" }).click();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(2);
  await expect(
    page.getByTestId("dw-stage-node").filter({ hasText: "Direct branch" })
  ).toBeVisible();
});

test("First use: a changed world persists across sign-out and sign-in", async ({ page }) => {
  const actor = uniqueActor("v2persist", "V2 Persist Owner");
  await register(page, actor);
  await createFirstWorld(page, "My first story world");

  const inspector = page.getByTestId("stage-inspector");
  await inspector.locator(".dw-live-check").first().click();
  await expect(inspector.getByText(/1\/1 complete/)).toBeVisible();

  await page.locator(".dw-profile summary").click();
  await page.getByRole("button", { name: "Sign out" }).click();
  await login(page, actor);

  await page.goto("/");
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
  await page.getByTestId("dw-stage-node").first().click();
  await expect(page.getByTestId("stage-inspector")).toContainText(/1\/1 complete/);
});

test("Narrow screen and reduced motion: canvas and inspector remain usable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  const actor = uniqueActor("v2directmobile", "V2 Direct Mobile");
  await register(page, actor);
  await createFirstWorld(page, "A mobile-first world");

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  const inspector = page.getByTestId("stage-inspector");
  await inspector.getByRole("tab", { name: "Branch", exact: true }).click();
  await expect(inspector.getByTestId("stage-branch-composer")).toBeVisible();
  const transition = await page
    .locator(".dw-world-node")
    .first()
    .evaluate((element) => getComputedStyle(element).transitionDuration);
  expect(Number.parseFloat(transition)).toBeLessThan(0.01);
});
