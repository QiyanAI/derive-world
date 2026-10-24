// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import {
  createInteractive,
  createInvitation,
  joinAsNewUser,
  register,
  uniqueActor
} from "../support/actors";

test.describe("two independent users, interactive object", () => {
  test("votes move between options, the owner closes the poll, and history persists", async ({
    page,
    browser,
    baseURL
  }) => {
    const actorA = uniqueActor("polA");
    const actorB = uniqueActor("polB");
    const title = `Interactive world ${actorA.handle.slice(-6)}`;
    const options = ["Option Alpha", "Option Beta", "Option Gamma"];

    await register(page, actorA);
    const objectID = await createInteractive(page, {
      title,
      prompt: "Which direction should the living world take next?",
      options
    });
    await expect(page.getByRole("heading", { name: title })).toBeVisible();

    const inviteLink = await createInvitation(page, objectID, [
      "Read",
      "Contribute",
      "Derive"
    ]);
    const { context: contextB, page: pageB } = await joinAsNewUser(
      browser,
      baseURL!,
      actorB,
      inviteLink
    );
    try {
      const alpha = pageB.locator(".option-button").filter({ hasText: "Option Alpha" });
      const beta = pageB.locator(".option-button").filter({ hasText: "Option Beta" });
      await alpha.click();
      await expect(pageB.getByText("Your vote: Option Alpha")).toBeVisible();
      await expect(alpha.locator(".option-tally strong")).toHaveText("1");

      await beta.click();
      await expect(pageB.getByText("Your vote: Option Beta")).toBeVisible();
      await expect(alpha.locator(".option-tally strong")).toHaveText("0");
      await expect(beta.locator(".option-tally strong")).toHaveText("1");

      // A sees the authentic tally after a server read.
      await page.getByTestId("object-refresh").click();
      await page.getByRole("link", { name: "Work", exact: true }).click();
      const alphaA = page.locator(".option-button").filter({ hasText: "Option Alpha" });
      const betaA = page.locator(".option-button").filter({ hasText: "Option Beta" });
      await expect(alphaA.locator(".option-tally strong")).toHaveText("0");
      await expect(betaA.locator(".option-tally strong")).toHaveText("1");

      await page.getByRole("button", { name: "Close poll" }).click();
      await expect(page.getByText("Voting closed")).toBeVisible();
      await pageB.getByTestId("object-refresh").click();
      await expect(pageB.getByText("Voting closed")).toBeVisible();
      await expect(pageB.locator(".option-button").first()).toBeDisabled();

      // B derives from the interactive object after voting.
      await pageB.getByTestId("derive-link").click();
      await pageB.getByLabel("Title of the new branch", { exact: true }).fill(
        `Poll branch ${actorB.handle.slice(-6)}`
      );
      await pageB.getByRole("button", { name: "Create derived object" }).click();
      await expect(pageB.getByText("Branch created.")).toBeVisible();

      // Versioned history is visible on the parent, including vote events.
      await page.goto(`/objects/${objectID}?tab=history`);
      await expect(page.getByRole("heading", { name: "Version history" })).toBeVisible();
      await expect(page.getByText("Cast vote").first()).toBeVisible();
      await expect(page.getByText("Close poll").first()).toBeVisible();
      await expect(page.locator(".version-list")).toContainText("v1");
    } finally {
      await contextB.close();
    }
  });
});
