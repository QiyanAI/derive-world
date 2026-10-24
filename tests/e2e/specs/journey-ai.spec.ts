// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import {
  acceptNextDialog,
  createCollaborative,
  createInvitation,
  joinAsNewUser,
  register,
  uniqueActor
} from "../support/actors";

test.describe("Handoff, Personal AI proposal, human confirmation, derive", () => {
  test("B's AI proposes; B approves; the server executes a confirmed derive", async ({
    page,
    browser,
    baseURL
  }) => {
    const actorA = uniqueActor("aiA");
    const actorB = uniqueActor("aiB");
    const title = `AI relay ${actorA.handle.slice(-6)}`;

    await register(page, actorA);
    const objectID = await createCollaborative(page, {
      title,
      summary: "An object where a Personal AI proposes within a member's scope.",
      body: "Human body before the AI turn."
    });

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
      await pageB.getByRole("link", { name: "AI collaboration" }).click();
      const aiPanel = pageB.getByTestId("ai-panel");
      await expect(aiPanel).toBeVisible();
      await expect(aiPanel.getByText(/Fixture mode uses scripted replies/)).toBeVisible();

      // The Handoff preview is a membership-level projection and never includes
      // another user's private context.
      await pageB.goto(`/objects/${objectID}?tab=access`);
      await pageB
        .getByRole("button", { name: "Preview the membership-level projection for this session." })
        .click();
      await expect(pageB.getByTestId("handoff-preview")).toBeVisible();
      await expect(pageB.getByText("Permitted actions")).toBeVisible();

      // Proposal path: one body change, no hidden version pin.
      await pageB.goto(`/objects/${objectID}?tab=ai`);
      await pageB.getByRole("button", { name: "Ask AI to propose a body edit" }).click();
      await expect(
        pageB.getByText("Fixture structured turn: I propose one body edit for you to review.")
      ).toBeVisible();
      await expect(pageB.getByRole("heading", { name: "Proposals waiting for a human" })).toBeVisible();
      await acceptNextDialog(pageB);
      await pageB.getByRole("button", { name: "Apply proposal" }).click();
      await expect(pageB.getByText("Proposal applied.")).toBeVisible();
      await expect(pageB.getByText("Version 2", { exact: true }).first()).toBeVisible();

      // Confirmation path: a separate derive request, bound to the current
      // version, is associated with this object and approved by a human.
      await pageB.getByRole("button", { name: "Ask AI to propose a derive" }).click();
      const confirmation = pageB.getByTestId("confirmation-card");
      await expect(confirmation).toBeVisible();
      await expect(confirmation.getByText("Fixture derived child")).toBeVisible();
      await expect(confirmation.getByText("Bound to v2")).toBeVisible();
      await acceptNextDialog(pageB);
      await pageB.getByRole("button", { name: "Approve and execute" }).click();
      await expect(pageB.getByText(/Approval executed: created Fixture derived child at v1\./)).toBeVisible();
      await pageB.getByRole("link", { name: "Open created object" }).click();
      await expect(pageB).toHaveURL(/\/objects\/[0-9a-f-]{36}/);
      await expect(pageB.getByRole("heading", { name: "Fixture derived child" })).toBeVisible();
      await expect(pageB.getByText("Version 1", { exact: true }).first()).toBeVisible();

      // B can see the child's origin. A cannot read B's private child, so the
      // parent's lineage clips the branch instead of naming it.
      await pageB.getByRole("link", { name: "View lineage" }).click();
      await expect(pageB.getByText(title).first()).toBeVisible();
      await page.goto(`/objects/${objectID}/lineage`);
      await expect(page.getByText("No branches are visible to you.")).toBeVisible();
    } finally {
      await contextB.close();
    }
  });
});
