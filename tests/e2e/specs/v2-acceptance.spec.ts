// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Browser, type Page } from "@playwright/test";
import { register, uniqueActor, fillStable } from "../support/actors";

async function contextPage(browser: Browser): Promise<{ page: Page; close: () => Promise<void> }> {
  const context = await browser.newContext({ locale: "en-US" });
  const page = await context.newPage();
  return { page, close: () => context.close() };
}

async function createWorld(page: Page, title: string) {
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), title);
  await expect(page.getByLabel("What do you want to begin?")).toHaveValue(title);
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Idea scene" }).click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  await expect(page.getByTestId("stage-inspector")).toBeVisible();
  await page.locator(".dw-inspector-close").click();
}

async function becomeFriends(alice: Page, bob: Page, aliceActor: { handle: string; displayName: string }, bobActor: { handle: string }) {
  await alice.goto("/friends");
  await alice.locator(".dw-head-actions").getByRole("button", { name: "Find a friend" }).click();
  await alice.locator("#dw-friend-search").fill(bobActor.handle);
  await expect(alice.locator("#dw-friend-search")).toHaveValue(bobActor.handle);
  await expect(alice.getByRole("button", { name: "+ Add friend" })).toHaveCount(1, {
    timeout: 15_000
  });
  await alice.getByRole("button", { name: "+ Add friend" }).click();
  await expect(alice.getByText("Friend request sent. Waiting for them to accept.")).toBeVisible();

  await bob.goto("/friends");
  await expect(bob.locator(".dw-incoming-requests")).toContainText(/V2/);
  await bob.locator(".dw-incoming-requests").getByRole("button", { name: "Accept" }).click();
  await expect(bob.getByRole("button", { name: new RegExp(aliceActor.displayName) }).first()).toBeVisible();
}

test("V2 acceptance: DM invitation, co-creation, consent, public discovery and private derivation", async ({
  browser
}, testInfo) => {
  const alice = await contextPage(browser);
  const bob = await contextPage(browser);
  const visitor = await contextPage(browser);
  try {
    const actorA = uniqueActor("v2accepta", "V2 Accept Alice");
    const actorB = uniqueActor("v2acceptb", "V2 Accept Bob");
    const actorC = uniqueActor("v2acceptc", "V2 Accept Visitor");
    const worldTitle = `Shared acceptance world ${actorA.handle.slice(-4)}`;
    const contribution = `Bob's real contribution ${actorA.handle.slice(-4)}`;

    await register(alice.page, actorA);
    await createWorld(alice.page, worldTitle);
    await register(bob.page, actorB);
    await becomeFriends(alice.page, bob.page, actorA, actorB);

    // A direct message is real and readable by the recipient.
    await alice.page.goto("/friends");
    await alice.page.getByRole("button", { name: new RegExp(actorB.displayName) }).first().click();
    await alice.page.getByLabel("Message to friend").fill("Let's continue this world together.");
    await alice.page.getByRole("button", { name: "Send", exact: true }).click();
    await bob.page.reload();
    await bob.page.getByRole("button", { name: new RegExp(actorA.displayName) }).first().click();
    await expect(bob.page.getByText("Let's continue this world together.")).toBeVisible();

    // The world invitation is a separate authorization, delivered by DM.
    await alice.page.goto("/friends");
    await expect(alice.page.locator(".dw-collab-center")).toContainText(worldTitle);
    await alice.page.getByRole("button", { name: "Invite a friend" }).first().click();
    const inviteDialog = alice.page.getByRole("dialog");
    await inviteDialog.getByRole("button", { name: "Create a one-use invitation" }).click();
    const inviteLink = await inviteDialog.locator("#dw-invite-link").inputValue();
    expect(inviteLink).toContain("/invite/");
    await inviteDialog.getByRole("button", { name: "Send invite" }).first().click();
    await expect(inviteDialog).toContainText(/direct message/);
    await inviteDialog.locator(".dw-modal-close").click();

    await bob.page.goto(inviteLink);
    await expect(bob.page.getByRole("button", { name: "Accept invitation" })).toBeVisible();
    await bob.page.getByRole("button", { name: "Accept invitation" }).click();
    await expect(bob.page).toHaveURL(/\/objects\/[0-9a-f-]{36}/);

    // Bob now has a shared world and can contribute to it.
    await bob.page.goto("/friends");
    await expect(bob.page.locator(".dw-collab-center")).toContainText(worldTitle);
    await bob.page.getByRole("button", { name: "Give the world a new idea" }).click();
    const contributeDialog = bob.page.getByRole("dialog");
    await fillStable(contributeDialog.locator("#dw-contribution"), contribution);
    const leaveContribution = contributeDialog.getByRole("button", { name: "Leave my contribution" });
    await expect(leaveContribution).toBeEnabled();
    await leaveContribution.click();
    await expect(bob.page.getByText("written into the shared world")).toBeVisible({ timeout: 15_000 });

    // Alice sees the contribution in the same spatial world.
    await alice.page.goto("/");
    await alice.page.getByTestId("dw-stage-node").first().click();
    await expect(alice.page.getByTestId("stage-inspector")).toContainText(contribution);

    // Publishing is an explicit choice. With a contributor present, the
    // publication waits for consent instead of silently going public.
    await alice.page.locator(".dw-privacy-chip").click();
    const privacy = alice.page.getByRole("dialog");
    await privacy.getByRole("radio", { name: /Public and derivable/ }).click();
    await expect(privacy).toContainText(/waiting for contributor consent|pending|等待/);
    await privacy.locator(".dw-modal-close").click();

    await bob.page.goto("/");
    await bob.page.locator(".dw-pulse-mini").click();
    const pulse = bob.page.getByRole("dialog");
    if ((await pulse.locator(".dw-consent-section").count()) > 0) {
      await pulse.locator(".dw-consent-section").getByRole("button", { name: "Approve" }).click();
      await expect(bob.page.locator(".dw-toast")).toContainText(/approved|同意/);
      await expect(pulse.locator(".dw-consent-section")).toHaveCount(0);
    }
    await bob.page.locator(".dw-modal-close").click();

    // The third account discovers the public version and derives a private
    // independent branch.
    await register(visitor.page, actorC);
    await visitor.page.goto("/explore");
    const publicLink = visitor.page.getByRole("link", { name: new RegExp(worldTitle) }).first();
    await expect(publicLink).toBeVisible({ timeout: 15_000 });
    await publicLink.click();
    await expect(visitor.page.getByTestId("explore-detail")).toContainText(worldTitle);
    await visitor.page.getByRole("button", { name: "Create my branch" }).click();
    const derive = visitor.page.getByRole("dialog");
    await derive.getByRole("button", { name: "Create my branch" }).click();
    await expect(visitor.page).toHaveURL(/\/$/);
    await expect(visitor.page.getByTestId("dw-stage-node")).toHaveCount(1);
    await expect(visitor.page.locator(".dw-world-node").first()).toContainText("new branch");
    await expect(visitor.page.locator(".dw-privacy-chip")).toContainText("Only me");

    // The original world keeps its owner, members, and co-created content.
    await alice.page.reload();
    await alice.page.getByTestId("dw-stage-node").first().click();
    await expect(alice.page.getByTestId("stage-inspector")).toContainText(contribution);

    await alice.page.screenshot({
      path: testInfo.outputPath("v2-acceptance-owner.png"),
      fullPage: true
    });
    await visitor.page.screenshot({
      path: testInfo.outputPath("v2-acceptance-derived.png"),
      fullPage: true
    });
  } finally {
    await alice.close();
    await bob.close();
    await visitor.close();
  }
});
