// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { register, uniqueActor, fillStable } from "../support/actors";

async function createScrollableWorld(page: Page, title: string) {
  await page.goto("/");
  await page.getByRole("button", { name: "Create a beginning" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), title);
  await expect(page.getByLabel("What do you want to begin?")).toHaveValue(title);
  await page.getByRole("button", { name: "Let it grow" }).click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();
  const input = inspector.getByLabel("Add a new idea or step");
  for (let index = 0; index < 12; index += 1) {
    const text = `Scroll item ${index + 1} that makes this panel taller`;
    const add = inspector.getByRole("button", { name: "Add", exact: true });
    let added = false;
    // A stage refresh can race a rapid sequence of controlled-input fills and
    // version-checked actions. Retry only after an unsuccessful server
    // response; a successful add is never repeated, so the exact row count
    // below still proves that no item was silently lost or duplicated.
    for (let attempt = 0; attempt < 6 && !added; attempt += 1) {
      await expect(async () => {
        await input.fill(text);
        await expect(input).toHaveValue(text);
      }).toPass({ timeout: 15_000 });
      await expect(add).toBeEnabled();
      const responsePromise = page.waitForResponse((response) => {
        const request = response.request();
        return request.url().includes("/actions") && (request.postData() ?? "").includes("add_element");
      });
      await add.click();
      const response = await responsePromise;
      added = response.ok();
    }
    expect(added).toBe(true);
    await expect(inspector.getByText(text).first()).toBeVisible();
  }
  // One opening element is created with the world; the loop adds twelve.
  await expect(inspector.locator(".dw-live-list > li")).toHaveCount(13);
  return inspector;
}

test("V2 scroll: the inspector scrolls, stays open, and never zooms the canvas", async ({ page }) => {
  const actor = uniqueActor("v2scroll", "V2 Scroll Owner");
  await register(page, actor);
  const inspector = await createScrollableWorld(page, "A world with a scrollable story");

  const scrollable = await inspector.evaluate(
    (element) => element.scrollHeight > element.clientHeight + 20
  );
  expect(scrollable).toBe(true);

  const viewport = page.getByTestId("dw-stage-viewport");
  const zoomBefore = await viewport.getAttribute("data-zoom");
  const list = inspector.locator(".dw-live-list");
  const box = await list.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move((box?.x ?? 0) + (box?.width ?? 0) / 2, (box?.y ?? 0) + 30);
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
  await page.getByRole("button", { name: "Create a beginning" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "A touch scroll world");
  await page.getByRole("button", { name: "Let it grow" }).click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();

  const touchAction = await inspector.evaluate((element) => getComputedStyle(element).touchAction);
  expect(touchAction).toBe("pan-y");
  const panTouchAction = await page
    .locator(".dw-stage-pan-layer")
    .evaluate((element) => getComputedStyle(element).touchAction);
  expect(panTouchAction).toBe("none");
});
