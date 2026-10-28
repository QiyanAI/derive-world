// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Browser, type Page } from "@playwright/test";
import { register, uniqueActor, fillStable } from "../support/actors";

async function newContextPage(browser: Browser): Promise<{ page: Page; close: () => Promise<void> }> {
  const context = await browser.newContext({ locale: "en-US" });
  const page = await context.newPage();
  return { page, close: () => context.close() };
}

async function createWorld(page: Page, title: string) {
  await page.goto("/");
  await expect(page.getByTestId("world-stage")).toBeVisible();
  await page.getByRole("button", { name: "Create a beginning" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), title);
  await expect(page.getByLabel("What do you want to begin?")).toHaveValue(title);
  await page.getByRole("button", { name: "Let it grow" }).click();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
  await expect(page.getByTestId("stage-inspector")).toBeVisible();
}

test("V2 world: real create, in-place edit, drag persisted, branch, zoom and AI settings", async ({
  page
}, testInfo) => {
  const actor = uniqueActor("v2world", "V2 World Owner");
  const title = `A city that listens ${actor.handle.slice(-4)}`;
  await register(page, actor);
  await createWorld(page, title);

  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector.getByRole("tab", { name: "Work", exact: true })).toHaveAttribute(
    "aria-selected",
    "true"
  );
  await inspector.getByLabel("Add a new idea or step").fill("The first rain remembers every voice");
  await inspector.getByRole("button", { name: "Add", exact: true }).click();
  await expect(inspector.getByText("The first rain remembers every voice")).toBeVisible();
  await inspector.locator(".dw-live-check").first().click();
  await expect(inspector.getByText(/1\/2 complete/)).toBeVisible();

  // Close the inspector, then drag the node. The position must survive reload.
  await inspector.locator(".dw-inspector-close").click();
  const node = page.getByTestId("dw-stage-node").first();
  await expect(node).toBeVisible();
  const before = await node.boundingBox();
  expect(before).not.toBeNull();
  const centerX = (before?.x ?? 0) + (before?.width ?? 0) / 2;
  const centerY = (before?.y ?? 0) + (before?.height ?? 0) / 2;
  await page.mouse.move(centerX, centerY);
  await page.mouse.down();
  await page.mouse.move(centerX + 150, centerY + 70, { steps: 10 });
  await page.mouse.up();
  await page.waitForTimeout(900);
  const after = await node.boundingBox();
  expect(after).not.toBeNull();
  expect(Math.abs((after?.x ?? 0) - (before?.x ?? 0))).toBeGreaterThan(30);
  const savedLayout = await page.evaluate(async () => {
    const response = await fetch("/api/v1/world/layout", { credentials: "include" });
    return (await response.json()) as { items: Array<{ x: number; y: number }> };
  });
  expect(savedLayout.items).toHaveLength(1);
  expect(savedLayout.items[0]?.x).toBeGreaterThan(800);
  await page.reload();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
  const reloadedLayout = await page.evaluate(async () => {
    const response = await fetch("/api/v1/world/layout", { credentials: "include" });
    return (await response.json()) as { items: Array<{ x: number; y: number }> };
  });
  expect(reloadedLayout.items[0]?.x).toBeCloseTo(savedLayout.items[0]?.x ?? 0, 1);

  // Zoom is real view state and resets to a fitted view.
  const viewport = page.getByTestId("dw-stage-viewport");
  const initialZoom = Number(await viewport.getAttribute("data-zoom"));
  await page.getByTestId("dw-stage-zoom-in").click();
  await expect.poll(async () => Number(await viewport.getAttribute("data-zoom"))).toBeGreaterThan(initialZoom);
  await page.getByTestId("dw-stage-zoom-reset").click();

  // Branch creation creates a real second node through the server.
  await page.getByTestId("dw-stage-node").first().click();
  const reopened = page.getByTestId("stage-inspector");
  await reopened.getByRole("tab", { name: "Branch", exact: true }).click();
  await reopened.getByLabel("Branch title").fill("A different rain");
  await reopened.getByRole("button", { name: "Preview branch" }).click();
  await expect(reopened.getByTestId("stage-branch-preview")).toBeVisible();
  await reopened.getByRole("button", { name: "Create this branch" }).click();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(2);
  await expect(page.getByTestId("dw-stage-node").filter({ hasText: "A different rain" })).toBeVisible();

  // AI settings are server-managed and never a browser key form.
  await page.locator(".dw-ai-entry").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("Connect your Personal AI");
  await expect(dialog).toContainText("never reads, echoes, or stores deployment API keys");
  await expect(dialog.locator('input[type="password"]')).toHaveCount(0);
  await dialog.locator(".dw-modal-close").click();
  await page.screenshot({ path: testInfo.outputPath("v2-world.png"), fullPage: true });
});

test("V2 together: real nickname search, request, accept and direct messages", async ({ browser }, testInfo) => {
  const first = await newContextPage(browser);
  const second = await newContextPage(browser);
  try {
    const alice = uniqueActor("v2alice", "V2 Alice");
    const bob = uniqueActor("v2bob", "V2 Bob");
    await register(first.page, alice);
    await register(second.page, bob);

    await first.page.goto("/friends");
    await expect(first.page.getByTestId("friends-page")).toBeVisible();
    await first.page.locator(".dw-head-actions").getByRole("button", { name: "Find a friend" }).click();
    const search = first.page.locator("#dw-friend-search");
    await search.fill(bob.handle);
    await expect(search).toHaveValue(bob.handle);
    await expect(first.page.getByRole("button", { name: "+ Add friend" })).toHaveCount(1, {
      timeout: 15_000
    });
    await first.page.getByRole("button", { name: "+ Add friend" }).click();
    await expect(first.page.getByText("Friend request sent. Waiting for them to accept.")).toBeVisible();

    await second.page.goto("/friends");
    await expect(second.page.locator(".dw-incoming-requests")).toContainText(alice.displayName);
    await second.page.locator(".dw-incoming-requests").getByRole("button", { name: "Accept" }).click();
    await expect(second.page.getByText(new RegExp(`${alice.displayName}`)).first()).toBeVisible();

    await first.page.reload();
    await expect(first.page.getByRole("button", { name: new RegExp(bob.displayName) }).first()).toBeVisible();
    await first.page.getByRole("button", { name: new RegExp(bob.displayName) }).first().click();
    await first.page.getByLabel("Message to friend").fill("Want to build this together?");
    await first.page.getByRole("button", { name: "Send", exact: true }).click();
    await expect(first.page.getByText("Want to build this together?")).toBeVisible();

    await second.page.reload();
    await second.page.getByRole("button", { name: new RegExp(alice.displayName) }).first().click();
    await expect(second.page.getByText("Want to build this together?")).toBeVisible();

    // Friendship does not grant world access: Bob has no shared world.
    await second.page.goto("/friends");
    await expect(second.page.locator(".dw-collab-center-empty")).toBeVisible();
    await second.page.screenshot({ path: testInfo.outputPath("v2-together.png"), fullPage: true });
  } finally {
    await first.close();
    await second.close();
  }
});

test("V2 explore: a deliberate public version can be discovered and derived privately", async ({
  browser
}, testInfo) => {
  const owner = await newContextPage(browser);
  const visitor = await newContextPage(browser);
  try {
    const actorA = uniqueActor("v2pub", "V2 Publisher");
    const actorB = uniqueActor("v2deriv", "V2 Derivative");
    const title = `Public rain world ${actorA.handle.slice(-4)}`;
    await register(owner.page, actorA);
    await createWorld(owner.page, title);

    await owner.page.getByTestId("dw-stage-node").first().click();
    await owner.page.locator(".dw-privacy-chip").click();
    const privacy = owner.page.getByRole("dialog");
    await privacy.getByRole("radio", { name: /Public and derivable/ }).click();
    await expect(privacy).toContainText(/published|主动公开/);
    await privacy.locator(".dw-modal-close").click();

    await owner.page.locator(".dw-profile summary").click();
    await owner.page.getByRole("button", { name: "Sign out" }).click();

    await register(visitor.page, actorB);
    await visitor.page.goto("/explore");
    const hero = visitor.page.locator(".dw-explore-hero");
    await expect(hero).toContainText(title);
    await hero.getByRole("link", { name: "Enter this world" }).click();
    await expect(visitor.page.getByTestId("explore-detail")).toContainText(title);
    await visitor.page.getByRole("button", { name: "Create my branch" }).click();
    const deriveDialog = visitor.page.getByRole("dialog");
    await expect(deriveDialog).toContainText("private space by default");
    await deriveDialog.getByRole("button", { name: "Create my branch" }).click();
    await expect(visitor.page).toHaveURL(/\/$/);
    await expect(visitor.page.getByTestId("dw-stage-node")).toHaveCount(1);
    await expect(visitor.page.locator(".dw-world-node").first()).toContainText("my branch");
    await expect(visitor.page.locator(".dw-privacy-chip")).toContainText("Only me");
    await visitor.page.screenshot({ path: testInfo.outputPath("v2-derived-private.png"), fullPage: true });
  } finally {
    await owner.close();
    await visitor.close();
  }
});

test("V2 narrow screen: bottom navigation, canvas and inspector stay usable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  const actor = uniqueActor("v2mobile", "V2 Mobile Owner");
  await register(page, actor);
  await page.goto("/");
  await expect(page.getByRole("navigation", { name: "Primary navigation" })).toBeVisible();
  await page.getByRole("button", { name: "Create a beginning" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "A small mobile world");
  await expect(page.getByLabel("What do you want to begin?")).toHaveValue("A small mobile world");
  await page.getByRole("button", { name: "Let it grow" }).click();
  await expect(page.getByTestId("stage-inspector")).toBeVisible();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  await page.getByTestId("stage-inspector").getByRole("tab", { name: "Branch", exact: true }).click();
  await expect(page.getByTestId("stage-branch-composer")).toBeVisible();
  await page.screenshot({ path: "test-results/v2-mobile.png", fullPage: true });
});
