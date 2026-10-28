// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { login, register, uniqueActor, fillStable } from "../support/actors";

async function createFirstWorld(page: import("@playwright/test").Page, title: string) {
  await page.goto("/");
  await expect(page.getByTestId("world-onboarding")).toBeVisible();
  await page.getByRole("button", { name: "Create the first world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), title);
  await page.getByRole("button", { name: "Let it grow" }).click();
  await expect(page.getByTestId("stage-inspector")).toBeVisible();
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
}

// The sign-out/sign-in test needs a persisted world, not a second exercise of
// the create dialog (the first test owns that). Create its fixture through the
// real object API so a stale dialog from the previous session cannot race it.
async function createWorldFixture(page: import("@playwright/test").Page, title: string) {
  const status = await page.evaluate(async (title) => {
    const csrf = document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith("dw_csrf="));
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID()
    };
    if (csrf) {
      headers["X-CSRF-Token"] = decodeURIComponent(csrf.slice("dw_csrf=".length));
    }
    const response = await fetch("/api/v1/objects", {
      method: "POST",
      headers,
      credentials: "include",
      body: JSON.stringify({
        object_type: "living_scene",
        title,
        content: {
          purpose: "project",
          premise: "",
          elements: [{ kind: "idea", text: "Opening: let this world begin to breathe" }],
          tags: []
        },
        shared_context: {}
      })
    });
    return response.status;
  }, title);
  expect(status).toBe(201);
  await page.goto("/");
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
  await page.getByTestId("dw-stage-node").first().click();
  await expect(page.getByTestId("stage-inspector")).toBeVisible();
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

test("A changed world persists across sign-out and sign-in", async ({ page }) => {
  const actor = uniqueActor("v2persist", "V2 Persist Owner");
  await register(page, actor);
  await createWorldFixture(page, "My first story world");

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
