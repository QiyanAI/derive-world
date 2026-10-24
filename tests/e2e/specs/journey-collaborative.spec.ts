// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import {
  createCollaborative,
  createInvitation,
  editBody,
  joinAsNewUser,
  register,
  uniqueActor
} from "../support/actors";

test.describe("two independent users, collaborative Derive Loop", () => {
  test("A invites B; B edits and derives; A observes the persisted version", async ({
    page,
    browser,
    baseURL
  }) => {
    const actorA = uniqueActor("colA");
    const actorB = uniqueActor("colB");
    const title = `Collaborative world ${actorA.handle.slice(-6)}`;

    await register(page, actorA);
    await expect(page.getByRole("heading", { name: "Your Digital Self is live" })).toBeVisible();
    await page.getByRole("link", { name: "Create your first object" }).click();
    await expect(page).toHaveURL(/\/objects\/new/);

    const objectID = await createCollaborative(page, {
      title,
      summary: "A shared object built for the two-user acceptance run.",
      body: "Initial body written by A."
    });
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
    await editBody(page, "A updated the body at version 2.");

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
      await expect(pageB.getByRole("heading", { name: title })).toBeVisible();
      await editBody(pageB, "B changed the body after joining.");

      // A sees B's persisted change after an explicit server read.
      await page.getByTestId("object-refresh").click();
      await page.getByRole("link", { name: "Work", exact: true }).click();
      await expect(page.getByText("B changed the body after joining.")).toBeVisible();
      await expect(page.getByText("Version 3", { exact: true }).first()).toBeVisible();

      // B derives an independent child from the current version.
      await pageB.getByTestId("derive-link").click();
      await expect(pageB.getByRole("heading", { name: "Grow a branch" })).toBeVisible();
      const childTitle = `B branch ${actorB.handle.slice(-6)}`;
      await pageB.getByLabel("Title of the new branch", { exact: true }).fill(childTitle);
      await pageB.getByRole("button", { name: "Create derived object" }).click();
      await expect(pageB).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
      await expect(pageB.getByText("Branch created.")).toBeVisible();
      await expect(pageB.getByText("Version 1", { exact: true }).first()).toBeVisible();

      // The lineage map shows the source object and the new branch.
      await pageB.getByRole("link", { name: "View lineage" }).click();
      await expect(pageB.getByRole("heading", { name: "Lineage" })).toBeVisible();
      await expect(pageB.getByText(title).first()).toBeVisible();
      await expect(pageB.getByText(childTitle).first()).toBeVisible();
    } finally {
      await contextB.close();
    }
  });
});
