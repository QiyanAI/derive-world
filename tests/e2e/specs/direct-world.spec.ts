// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { login, register, uniqueActor } from "../support/actors";

test("First use: choose a project, change it in place, and create a real branch on the stage", async ({
  page
}) => {
  const actor = uniqueActor("r1direct", "R1 Direct Owner");
  await register(page, actor);

  await expect(page.getByRole("heading", { name: "What do you want to make real?" })).toBeVisible();
  await expect(page.getByRole("link", { name: /Make a project real/ })).toBeVisible();
  await expect(page.getByRole("link", { name: /Build a world of your own/ })).toBeVisible();
  await expect(page.getByText(/Objects you create are real and saved/)).toBeVisible();

  await page.getByRole("link", { name: /Make a project real/ }).click();
  await expect(page).toHaveURL(/objects\/new\?template=living_scene&purpose=project&first=1/);
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("My first real project");
  await page.getByRole("button", { name: "Create object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);

  // The first-loop guide is visible and describes the real loop, not a fake demo.
  await expect(page.getByTestId("first-loop-guide")).toBeVisible();
  await expect(page.getByTestId("first-loop-guide")).toContainText("Your first loop");

  // Make the first real change in the work panel.
  const scenePanel = page.getByTestId("scene-work-panel");
  await expect(scenePanel.getByText("Choose the first small outcome")).toBeVisible();
  await scenePanel.locator(".scene-add select").selectOption("step");
  await scenePanel
    .getByPlaceholder("Write the next idea, step, scene, choice, or outcome")
    .fill("Run one small test with a real user");
  await scenePanel.getByRole("button", { name: "Add element" }).click();
  await expect(scenePanel.getByText("Run one small test with a real user")).toBeVisible();
  await expect(scenePanel.getByText(/0 of 3 steps done/)).toBeVisible();

  // Enter the stage and select the object without navigating to a management page.
  await page.goto("/");
  const stage = page.getByTestId("world-stage");
  await expect(stage).toBeVisible();
  await expect(stage.getByTestId("stage-continue")).toBeVisible();
  await stage.getByRole("button", { name: /Select My first real project/ }).click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();
  await expect(inspector.getByRole("tab", { name: "Work", exact: true })).toHaveAttribute(
    "aria-selected",
    "true"
  );

  // Edit in place from the stage inspector.
  await inspector.locator(".scene-add select").selectOption("step");
  await inspector
    .getByPlaceholder("Write the next idea, step, scene, choice, or outcome")
    .fill("Ship the first outcome");
  await inspector.getByRole("button", { name: "Add element" }).click();
  await expect(inspector.getByText("Ship the first outcome")).toBeVisible();
  await expect(inspector.getByText(/0 of 4 steps done/)).toBeVisible();

  // Preview a branch: nothing exists before the human confirmation.
  await inspector.getByRole("tab", { name: "Branch", exact: true }).click();
  await expect(inspector.getByTestId("stage-branch-composer")).toBeVisible();
  await inspector.getByLabel("Branch title").fill("Direct branch");
  await inspector.getByRole("button", { name: "Preview branch" }).click();
  await expect(inspector.getByTestId("stage-branch-preview")).toBeVisible();
  await expect(inspector.getByTestId("stage-branch-preview")).toContainText("Ship the first outcome");
  await expect(stage.locator(".stage-roots").getByText("Direct branch")).toHaveCount(0);

  // Confirm creates a real server branch and the new node appears on the stage.
  await inspector.getByRole("button", { name: "Create this branch" }).click();
  await expect(stage.locator(".stage-roots").getByText("Direct branch")).toBeVisible();
  await expect(stage.locator(".stage-roots").getByText("Direct branch")).toHaveCount(1);

  // Keyboard: Escape closes the inspector and returns focus to the trigger.
  await inspector.press("Escape");
  await expect(page.getByTestId("stage-inspector")).toHaveCount(0);
  await expect(stage.getByRole("button", { name: /Select Direct branch/ })).toBeFocused();
});

test("First use: a free story path persists across sign-out and Continue", async ({ page }) => {
  const actor = uniqueActor("r1story", "R1 Story Owner");
  await register(page, actor);
  await page.getByRole("link", { name: /Build a world of your own/ }).click();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("My first story world");
  await page.getByRole("button", { name: "Create object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);

  const scenePanel = page.getByTestId("scene-work-panel");
  await expect(scenePanel.getByText("A quiet beginning where everything can still change")).toBeVisible();
  await expect(scenePanel.getByText(/0 of 2 elements resolved/)).toBeVisible();
  await scenePanel.getByRole("checkbox", { name: "Resolved" }).first().click();
  await expect(scenePanel.getByText(/1 of 2 elements resolved/)).toBeVisible();

  await page.goto("/");
  await expect(page.getByTestId("stage-continue")).toBeVisible();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("link", { name: "Create your Digital Self" }).first()).toBeVisible();
  await login(page, actor);
  await page.goto("/");
  await expect(page.getByText("My first story world").first()).toBeVisible();
  await page.getByTestId("stage-continue").getByRole("link", { name: "Continue in place" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}\?tab=work/);
  await expect(page.getByTestId("scene-work-panel").getByText(/1 of 2 elements resolved/)).toBeVisible();
});

test("Narrow screen and reduced motion: in-place selection and branch form remain usable", async ({
  page
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  const actor = uniqueActor("r1mobile", "R1 Mobile Owner");
  await register(page, actor);
  await page.getByRole("link", { name: /Make a project real/ }).click();
  await page.getByRole("button", { name: "Create object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
  await page.goto("/");
  const stage = page.getByTestId("world-stage");
  await stage.getByRole("button", { name: /Select My first real project/ }).click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();
  await inspector.getByRole("tab", { name: "Branch", exact: true }).click();
  await expect(inspector.getByTestId("stage-branch-composer")).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  const transition = await page
    .locator(".stage-node")
    .first()
    .evaluate((element) => getComputedStyle(element).transitionDuration);
  expect(Number.parseFloat(transition)).toBeLessThan(0.01);
});
