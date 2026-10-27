// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Browser } from "@playwright/test";
import { register, uniqueActor } from "../support/actors";

async function newActorPage(browser: Browser) {
  const context = await browser.newContext({ locale: "en-US" });
  const page = await context.newPage();
  return { context, page };
}

test("Living world: one person creates a scene, inspects the lens, publishes, and explores", async ({
  browser
}, testInfo) => {
  const { context, page } = await newActorPage(browser);
  try {
    const actor = uniqueActor("r1stage", "R1 Stage Owner");
    const sceneTitle = `Release ${actor.handle}`;
    await register(page, actor);
    await page.goto("/");
    await expect(page.getByTestId("world-stage")).toBeVisible();
    await expect(page.locator(".dw-world-hello")).toContainText(actor.displayName);

    await page.goto("/objects/new?template=living_scene&purpose=project");
    await expect(page.getByRole("radio", { name: /Living scene/ })).toBeChecked();
    await page.getByLabel("Title", { exact: true }).fill(sceneTitle);
    await page.getByLabel("Premise or current goal").fill("Ship one small useful outcome.");
    await page.getByPlaceholder("Write the next idea, step, scene, choice, or outcome").first().fill(
      "Interview five real users"
    );
    await page.getByRole("button", { name: "Create object" }).click();
    await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
    await expect(page.getByTestId("scene-work-panel")).toBeVisible();
    await expect(page.getByText("Interview five real users")).toBeVisible();

    const scenePanel = page.getByTestId("scene-work-panel");
    await scenePanel.locator(".scene-add select").selectOption("step");
    await scenePanel
      .getByPlaceholder("Write the next idea, step, scene, choice, or outcome")
      .fill("Write the release note");
    await scenePanel.getByRole("button", { name: "Add element" }).click();
    await expect(scenePanel.getByText("Write the release note")).toBeVisible();
    await scenePanel.getByRole("checkbox", { name: "Completed" }).last().click();
    await expect(scenePanel.getByText(/1 of 2 steps done/)).toBeVisible();

    await page.getByRole("link", { name: "Lens" }).click();
    await expect(page.getByTestId("lens-panel")).toBeVisible();
    await expect(page.getByText("Real versions")).toBeVisible();
    await expect(page.getByText("Visible branches")).toBeVisible();

    await page.getByRole("link", { name: "Publish" }).click();
    await expect(page.getByTestId("publication-panel")).toBeVisible();
    await page
      .getByRole("checkbox", { name: /Allow logged-in visitors/ })
      .check();
    await page.getByRole("button", { name: "Publish", exact: true }).click();
    await expect(page.locator(".publication-list")).toContainText("public");

    await page.goto("/explore");
    await expect(page.getByTestId("explore-page")).toBeVisible();
    const publicLink = page.getByRole("link", { name: new RegExp(sceneTitle) }).first();
    await expect(publicLink).toBeVisible();
    await publicLink.click();
    await expect(page.getByTestId("explore-detail")).toContainText(sceneTitle);
    await expect(page.getByTestId("explore-detail").getByRole("heading", { name: sceneTitle, level: 2 })).toBeVisible();
    await page.getByRole("button", { name: "Create my branch" }).click();
    await expect(page.getByRole("dialog")).toContainText("Create my branch");

    await page.goto("/");
    await page.screenshot({ path: testInfo.outputPath("living-world-stage.png"), fullPage: true });
    await page.goto("/explore");
    await page.screenshot({ path: testInfo.outputPath("living-world-explore.png"), fullPage: true });
  } finally {
    await context.close();
  }
});

test("Living world: two people become friends and exchange direct messages", async ({ browser }, testInfo) => {
  const first = await newActorPage(browser);
  const second = await newActorPage(browser);
  try {
    const alice = uniqueActor("r1alice", "R1 Alice");
    const bob = uniqueActor("r1bob", "R1 Bob");
    await register(first.page, alice);
    await register(second.page, bob);

    await first.page.goto("/friends");
    await expect(first.page.getByTestId("friends-page")).toBeVisible();
    await first.page.locator(".dw-head-actions").getByRole("button", { name: "Find a friend" }).click();
    await first.page.locator("#dw-friend-search").fill(bob.handle);
    await expect(first.page.locator("#dw-friend-search")).toHaveValue(bob.handle);
    await expect(first.page.getByRole("button", { name: "+ Add friend" })).toBeVisible({ timeout: 15_000 });
    await first.page.getByRole("button", { name: "+ Add friend" }).click();
    await expect(first.page.getByText("Friend request sent. Waiting for them to accept.")).toBeVisible();

    await second.page.goto("/friends");
    await expect(second.page.locator(".dw-incoming-requests")).toContainText(alice.displayName);
    await second.page.locator(".dw-incoming-requests").getByRole("button", { name: "Accept" }).click();
    await expect(second.page.getByRole("button", { name: new RegExp(alice.displayName) }).first()).toBeVisible();

    await first.page.reload();
    await first.page.getByRole("button", { name: new RegExp(bob.displayName) }).first().click();
    await first.page.getByLabel("Message to friend").fill("Want to build this together?");
    await first.page.getByRole("button", { name: "Send", exact: true }).click();
    await expect(first.page.getByText("Want to build this together?")).toBeVisible();

    await second.page.reload();
    await second.page.getByRole("button", { name: new RegExp(alice.displayName) }).first().click();
    await expect(second.page.getByText("Want to build this together?")).toBeVisible();
    await second.page.screenshot({ path: testInfo.outputPath("living-world-friends.png"), fullPage: true });
  } finally {
    await first.context.close();
    await second.context.close();
  }
});
