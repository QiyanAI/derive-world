// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import {
  acceptInvitation,
  acceptNextDialog,
  createCollaborative,
  createInvitation,
  editBody,
  login,
  register,
  registerFromInvite,
  uniqueActor
} from "../support/actors";

test.describe("security negatives and honest failure states", () => {
  test("cross-user access is denied and private context never crosses users", async ({
    page,
    browser,
    baseURL
  }) => {
    const actorA = uniqueActor("secA");
    const actorB = uniqueActor("secB");
    await register(page, actorA);
    const objectID = await createCollaborative(page, {
      title: `Private object ${actorA.handle.slice(-6)}`,
      summary: "Only members may read this object.",
      body: "A private body that B must not receive."
    });

    const contextB = await browser.newContext({ baseURL: baseURL! });
    const pageB = await contextB.newPage();
    try {
      await register(pageB, actorB);
      await pageB.goto(`/objects/${objectID}`);
      await expect(pageB.getByRole("heading", { name: "No access" })).toBeVisible();
      await expect(pageB.getByText("A private body that B must not receive.")).toHaveCount(0);

      // A grants B read+contribute. B still cannot derive.
      const inviteLink = await createInvitation(page, objectID, ["Read", "Contribute"]);
      await acceptInvitation(pageB, inviteLink);

      const sentinel = `PRIVATE-CONTEXT-${actorA.handle}`;
      await page.goto("/ai");
      await page.getByLabel("Private context", { exact: true }).fill(sentinel);
      await page.getByRole("button", { name: "Save private context" }).click();
      await expect(page.getByText("Private context saved.")).toBeVisible();

      await pageB.goto(`/objects/${objectID}?tab=ai`);
      await pageB.getByRole("button", { name: "Say hello" }).click();
      await expect(pageB.getByText(/Fixture reply: hello/)).toBeVisible();
      await expect(pageB.locator("body")).not.toContainText(sentinel);

      await pageB.goto(`/objects/${objectID}?tab=access`);
      await pageB
        .getByRole("button", { name: "Preview the membership-level projection for this session." })
        .click();
      await expect(pageB.getByTestId("handoff-preview")).toBeVisible();
      await expect(pageB.locator("body")).not.toContainText(sentinel);

      await pageB.goto(`/objects/${objectID}/derive`);
      await expect(pageB.getByText("Your role does not include derive.")).toBeVisible();
    } finally {
      await contextB.close();
    }
  });

  test("refused invitations and removed membership stop access immediately", async ({
    page,
    browser,
    baseURL
  }) => {
    const actorA = uniqueActor("revA");
    const actorB = uniqueActor("revB");
    await register(page, actorA);
    const objectID = await createCollaborative(page, {
      title: `Revocation object ${actorA.handle.slice(-6)}`,
      summary: "Invitation and membership revocation.",
      body: "A body that is only temporarily shared."
    });

    const contextB = await browser.newContext({ baseURL: baseURL! });
    const pageB = await contextB.newPage();
    try {
      const inviteLink = await createInvitation(page, objectID, ["Read", "Contribute"]);
      await pageB.goto(inviteLink);
      await registerFromInvite(pageB, actorB);
      await expect(pageB.getByRole("button", { name: "Accept invitation" })).toBeVisible();

      // The owner revokes the pending invitation before it is accepted.
      await page.goto(`/objects/${objectID}?tab=access`);
      await acceptNextDialog(page);
      await page.getByRole("button", { name: "Revoke" }).first().click();
      await pageB.goto(inviteLink);
      await expect(pageB.getByRole("heading", { name: "This invitation is not available." })).toBeVisible();

      // A fresh invitation works; after B joins, removing the member ends the
      // session's access on the next server read.
      const secondLink = await createInvitation(page, objectID, ["Read", "Contribute"]);
      await acceptInvitation(pageB, secondLink);
      await expect(pageB.getByRole("heading", { name: `Revocation object ${actorA.handle.slice(-6)}` })).toBeVisible();

      await page.goto(`/objects/${objectID}?tab=access`);
      const memberRow = page
        .getByTestId("member-list")
        .locator("li")
        .filter({ hasText: actorB.displayName });
      await expect(memberRow).toBeVisible();
      await acceptNextDialog(page);
      await memberRow.getByRole("button", { name: "Remove" }).click();
      await expect(memberRow).toHaveCount(0);

      await pageB.getByTestId("object-refresh").click();
      await expect(pageB.getByRole("heading", { name: "Your access to this object has ended." })).toBeVisible();
    } finally {
      await contextB.close();
    }
  });

  test("a stale version conflict is reported as an error, never as success", async ({
    page,
    browser,
    baseURL
  }) => {
    const actorA = uniqueActor("verA");
    await register(page, actorA);
    const objectID = await createCollaborative(page, {
      title: `Conflict object ${actorA.handle.slice(-6)}`,
      summary: "Two sessions edit the same version.",
      body: "Original body."
    });

    const contextTwo = await browser.newContext({ baseURL: baseURL! });
    const pageTwo = await contextTwo.newPage();
    try {
      await login(pageTwo, actorA);
      await page.goto(`/objects/${objectID}?tab=work`);
      await pageTwo.goto(`/objects/${objectID}?tab=work`);
      await page.getByRole("button", { name: "Edit document" }).click();
      await pageTwo.getByRole("button", { name: "Edit document" }).click();
      await page.getByRole("textbox", { name: "Body", exact: true }).fill("First session wins the version.");
      await page.getByRole("button", { name: "Save changes" }).click();
      await expect(page.getByText("Saved.")).toBeVisible();

      await pageTwo.getByRole("textbox", { name: "Body", exact: true }).fill("Second session is stale.");
      await pageTwo.getByRole("button", { name: "Save changes" }).click();
      await expect(pageTwo.locator('[data-error-code="version_conflict"]')).toBeVisible();
      await expect(pageTwo.getByText("Saved.")).toHaveCount(0);
      await expect(
        pageTwo.getByRole("textbox", { name: "Body", exact: true })
      ).toHaveValue("Second session is stale.");
    } finally {
      await contextTwo.close();
    }
  });

  test("capacity guards are byte-aware and a service failure is not a sign-out", async ({
    page,
    browser,
    baseURL
  }) => {
    const actorA = uniqueActor("capA");
    await register(page, actorA);
    await page.goto("/ai");
    const multibyte = "中".repeat(3000); // 9,000 UTF-8 bytes, 3,000 characters
    await page.getByLabel("Private context", { exact: true }).fill(multibyte);
    await expect(page.getByText(/9000 \/ 8192 bytes/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Save private context" })).toBeDisabled();

    const context = await browser.newContext({ baseURL: baseURL! });
    const failurePage = await context.newPage();
    try {
      await failurePage.route("**/api/v1/auth/me", (route) =>
        route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({
            error: {
              code: "internal",
              message: "synthetic upstream failure",
              request_id: "e2e-service-failure",
              details: {}
            }
          })
        })
      );
      await failurePage.goto("/");
      await expect(failurePage.locator('[data-error-code="internal"]')).toBeVisible();
      await expect(failurePage.getByRole("link", { name: "Sign in" })).toHaveCount(0);
      await failurePage.unroute("**/api/v1/auth/me");
      await failurePage.getByRole("button", { name: "Try again" }).click();
      await expect(failurePage.getByRole("heading", { name: "A living web of objects you and your AI can grow." })).toBeVisible();
    } finally {
      await context.close();
    }
  });
});
