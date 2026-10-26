// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { createCollaborative, register, uniqueActor } from "../support/actors";

test("Published untrusted title and body render as text, not executable HTML", async ({ page }) => {
  const actor = uniqueActor("r1xss", "R1 XSS Owner");
  const title = `<img src=x onerror="window.__xss=1"> XSS title ${actor.handle}`;
  const summary = `<script>window.__xss=2</script><img src=x onerror="window.__xss=3">XSS summary`;
  const body = `<script>window.__xss=4</script><img src=x onerror="window.__xss=5">XSS body`;
  await register(page, actor);
  await createCollaborative(page, {
    title,
    summary,
    body
  });

  await page.getByRole("link", { name: "Publish" }).click();
  await expect(page.getByTestId("publication-panel")).toBeVisible();
  await page.getByRole("checkbox", { name: /Allow logged-in visitors/ }).check();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(page.locator(".publication-list")).toContainText("public");

  await page.getByRole("button", { name: "Sign out" }).click();
  await page.goto("/explore");
  await expect(page.getByTestId("explore-page")).toBeVisible();
  await page.getByRole("link", { name: new RegExp(actor.handle) }).click();
  await expect(page).toHaveURL(/\/explore\/[0-9a-f-]{36}/);

  const publicWorld = page.locator(".public-world");
  await expect(page.getByRole("heading", { name: new RegExp(actor.handle) })).toBeVisible();
  await expect(publicWorld.getByText(summary, { exact: false })).toBeVisible();
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  const executed = await page.evaluate(
    () => (window as unknown as { __xss?: number }).__xss
  );
  expect(executed).toBeUndefined();
});
