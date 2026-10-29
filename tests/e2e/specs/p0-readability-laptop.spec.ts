// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import {
  expectNoHorizontalOverflow,
  fillStable,
  register,
  uniqueActor
} from "../support/actors";

// Owner acceptance scenario 7: on an ordinary laptop viewport and on a phone,
// an AI reply must be readable (contrast), the composer reachable, the panel
// stable while its transcript scrolls, and nothing may overflow horizontally.
test("P0 readability: AI messages, composer and panels at 1366x768 and 390x844", async ({
  page
}) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 1366, height: 768 });
  await register(page, uniqueActor("p0read", "P0 Readability Owner"));

  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "A readable room");
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  await expect(page.getByTestId("open-world-page")).toBeVisible({ timeout: 30_000 });
  await page.getByTestId("ai-entry").click();
  await fillStable(page.locator("#dw-ai-compose"), "Just say hello to this world");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(
    page.getByText("Hello, world. Nothing needs to change right now.")
  ).toBeVisible({ timeout: 30_000 });

  const ratio = await contrastRatio(page, ".ai-message.ai-assistant p");
  expect(ratio).toBeGreaterThanOrEqual(4.5);
  await expectNoHorizontalOverflow(page);

  const composer = await page.locator("#dw-ai-compose").boundingBox();
  expect(composer).not.toBeNull();
  expect((composer?.y ?? 9999) + (composer?.height ?? 0)).toBeLessThanOrEqual(768);

  // Scrolling the transcript must not close the panel or move the world.
  const messages = page.locator(".ai-messages");
  const beforeScroll = await messages.evaluate((element) => element.scrollTop);
  await messages.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  const afterScroll = await messages.evaluate((element) => element.scrollTop);
  expect(afterScroll).toBeGreaterThanOrEqual(beforeScroll);
  await expect(page.getByTestId("ai-panel")).toBeVisible();
  await expect(page.getByTestId("open-world-page")).toBeVisible();

  // The same reply stays readable on a phone.
  await page.setViewportSize({ width: 390, height: 844 });
  await expectNoHorizontalOverflow(page);
  const mobileRatio = await contrastRatio(page, ".ai-message.ai-assistant p");
  expect(mobileRatio).toBeGreaterThanOrEqual(4.5);

  // The global creation dialog keeps its composer inside a 768px viewport.
  await page.setViewportSize({ width: 1366, height: 768 });
  await page.goto("/");
  await page.getByTestId("personal-ai-entry").click();
  const dialogDraft = await page.locator("#dw-personal-ai-draft").boundingBox();
  expect(dialogDraft).not.toBeNull();
  expect((dialogDraft?.y ?? 9999) + (dialogDraft?.height ?? 0)).toBeLessThanOrEqual(768);
});

// WCAG contrast of the message text against the effective bubble background.
// The bubble background may be translucent, so opaque ancestors are composited
// from the page base upward before measuring.
async function contrastRatio(page: Page, selector: string): Promise<number> {
  return page.evaluate((target) => {
    function parseColor(value: string): [number, number, number, number] {
      const match = value.match(/rgba?\(([^)]+)\)/);
      if (!match) {
        return [0, 0, 0, 1];
      }
      const parts = match[1].split(",").map((part) => Number.parseFloat(part.trim()));
      return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0, parts[3] ?? 1];
    }
    function luminance(channels: number[]): number {
      const channel = (raw: number) => {
        const value = raw / 255;
        return value <= 0.03928 ? value / 12.92 : Math.pow((value + 0.055) / 1.055, 2.4);
      };
      return (
        0.2126 * channel(channels[0] ?? 0) +
        0.7152 * channel(channels[1] ?? 0) +
        0.0722 * channel(channels[2] ?? 0)
      );
    }
    const element = document.querySelector(target) as HTMLElement | null;
    if (!element) {
      return 0;
    }
    const layers: Array<[number, number, number, number]> = [];
    let node: HTMLElement | null = element;
    while (node) {
      const color = parseColor(getComputedStyle(node).backgroundColor);
      if (color[3] > 0) {
        layers.push(color);
      }
      if (color[3] >= 1) {
        break;
      }
      node = node.parentElement;
    }
    let base: [number, number, number] = [9, 13, 23];
    for (let index = layers.length - 1; index >= 0; index -= 1) {
      const layer = layers[index];
      if (!layer) {
        continue;
      }
      const [r, g, b, alpha] = layer;
      base = [
        r * alpha + base[0] * (1 - alpha),
        g * alpha + base[1] * (1 - alpha),
        b * alpha + base[2] * (1 - alpha)
      ];
    }
    const foreground = parseColor(getComputedStyle(element).color);
    const first = luminance([foreground[0], foreground[1], foreground[2]]);
    const second = luminance(base);
    return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
  }, selector);
}
