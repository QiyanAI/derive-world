// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

async function createScrollableWorld(page: Page, title: string) {
  // This spec is about inspector scrolling, not the create or add forms. Seed
  // one living scene with 25 elements through the real create API; the stage
  // node and inspector then render the same real product. One create call
  // also avoids the per-user API bucket that a rapid add loop can exhaust on
  // loaded CI runners.
  const created = await page.evaluate(async (worldTitle) => {
    const csrf = document.cookie
      .split("; ")
      .find((entry) => entry.startsWith("dw_csrf="))
      ?.split("=")[1];
    const idempotency = Array.from({ length: 40 }, () =>
      "abcdefghijklmnopqrstuvwxyz0123456789"[Math.floor(Math.random() * 36)]
    ).join("");
    const elements = Array.from({ length: 25 }, (_, index) => ({
      kind: "idea",
      text: `Scroll item ${index + 1} that makes this panel taller`
    }));
    const response = await fetch("/api/v1/objects", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": idempotency,
        ...(csrf ? { "X-CSRF-Token": csrf } : {})
      },
      body: JSON.stringify({
        object_type: "living_scene",
        title: worldTitle,
        content: { purpose: "project", premise: "", elements, tags: [] },
        shared_context: {}
      })
    });
    return {
      ok: response.ok,
      status: response.status,
      detail: response.ok ? "" : await response.text()
    };
  }, title);
  expect(created.ok, JSON.stringify(created)).toBe(true);

  await page.goto("/");
  const stageNode = page.getByTestId("dw-stage-node").filter({ hasText: title }).first();
  await expect(stageNode).toBeVisible();
  await stageNode.click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();
  await expect(inspector.locator(".dw-live-list > li")).toHaveCount(25);
  await expect
    .poll(
      () =>
        inspector.evaluate((element) => element.scrollHeight > element.clientHeight + 20),
      { timeout: 15_000 }
    )
    .toBe(true);
  return inspector;
}

test("V2 scroll: the inspector scrolls, stays open, and never zooms the canvas", async ({ page }) => {
  const actor = uniqueActor("v2scroll", "V2 Scroll Owner");
  await register(page, actor);
  const inspector = await createScrollableWorld(page, "A world with a scrollable story");

  const viewport = page.getByTestId("dw-stage-viewport");
  const zoomBefore = await viewport.getAttribute("data-zoom");
  const list = inspector.locator(".dw-live-list");
  await expect(list.locator("> li")).toHaveCount(25);
  await page.waitForTimeout(250);
  await list.hover({ position: { x: 20, y: 20 } });
  await page.mouse.wheel(0, 420);
  await page.waitForTimeout(300);

  // The panel is still the same panel, it actually scrolled, and the world
  // canvas did not zoom.
  await expect(inspector).toBeVisible();
  await expect(viewport).toHaveAttribute("data-zoom", zoomBefore ?? "");
  const scrollTop = await inspector.evaluate((element) => element.scrollTop);
  expect(scrollTop).toBeGreaterThan(0);

  // A pointer down on panel text is not a background click.
  await inspector.locator(".dw-inspect-sub").click();
  await expect(inspector).toBeVisible();

  // Closing still works through the real close control.
  await inspector.locator(".dw-inspector-close").click();
  await expect(inspector).toHaveCount(0);

  // Reopen, then a deliberate background click still closes the inspector.
  await page.getByTestId("dw-stage-node").first().click();
  const reopened = page.getByTestId("stage-inspector");
  await expect(reopened).toBeVisible();
  const stage = page.getByTestId("world-stage");
  const stageBox = await stage.boundingBox();
  expect(stageBox).not.toBeNull();
  await page.mouse.click((stageBox?.x ?? 0) + 30, (stageBox?.y ?? 0) + (stageBox?.height ?? 0) / 2);
  await expect(reopened).toHaveCount(0);
});

test("V2 touch: the inspector opts into native vertical scrolling", async ({ page }) => {
  const actor = uniqueActor("v2touchscroll", "V2 Touch Scroll Owner");
  await register(page, actor);
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "A touch scroll world");
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Idea scene" }).click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();

  const touchAction = await inspector.evaluate((element) => getComputedStyle(element).touchAction);
  expect(touchAction).toBe("pan-y");
  const panTouchAction = await page
    .locator(".dw-stage-pan-layer")
    .evaluate((element) => getComputedStyle(element).touchAction);
  expect(panTouchAction).toBe("none");
});
