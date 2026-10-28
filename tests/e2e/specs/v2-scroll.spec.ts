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

  // This spec is about inspector scrolling, not the add form. Seed the twelve
  // rows through the same real action API so a stage refresh cannot race a
  // rapid sequence of controlled-input fills on a loaded CI runner. One
  // opening element is created with the world, so the final list has 13 rows.
  const object = await page.evaluate(async (worldTitle) => {
    const response = await fetch("/api/v1/objects?scope=owned", {
      credentials: "same-origin"
    });
    const payload = (await response.json()) as {
      items: Array<{ id: string; title: string; version: number }>;
    };
    return payload.items.find((item) => item.title === worldTitle) ?? null;
  }, title);
  expect(object).not.toBeNull();
  if (!object) {
    return inspector;
  }
  const seeded = await page.evaluate(
    async ({ id, version }) => {
      const csrf = document.cookie
        .split("; ")
        .find((entry) => entry.startsWith("dw_csrf="))
        ?.split("=")[1];
      let current = version;
      for (let index = 0; index < 12; index += 1) {
        const idempotency = Array.from({ length: 40 }, () =>
          "abcdefghijklmnopqrstuvwxyz0123456789"[Math.floor(Math.random() * 36)]
        ).join("");
        const response = await fetch(`/api/v1/objects/${id}/actions`, {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": idempotency,
            ...(csrf ? { "X-CSRF-Token": csrf } : {})
          },
          body: JSON.stringify({
            action: "add_element",
            input: { kind: "idea", text: `Scroll item ${index + 1} that makes this panel taller` },
            expected_version: current
          })
        });
        if (!response.ok) {
          return { ok: false, status: response.status };
        }
        const updated = (await response.json()) as { version: number };
        current = updated.version;
      }
      return { ok: true, status: 200 };
    },
    { id: object.id, version: object.version }
  );
  expect(seeded.ok).toBe(true);

  await page.reload();
  const stageNode = page.getByTestId("dw-stage-node").filter({ hasText: title }).first();
  await expect(stageNode).toBeVisible();
  await stageNode.click();
  await expect(inspector).toBeVisible();
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
  await expect(list.locator("> li")).toHaveCount(13);
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
