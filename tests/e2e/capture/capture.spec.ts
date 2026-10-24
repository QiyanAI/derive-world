// SPDX-License-Identifier: Apache-2.0
import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import {
  acceptInvitation,
  acceptNextDialog,
  createCollaborative,
  createInteractive,
  createInvitation,
  editBody,
  register,
  registerFromInvite,
  uniqueActor
} from "../support/actors";

const assets = path.join(process.cwd(), "docs/assets");
const baseURL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:8080";

async function shot(page: import("@playwright/test").Page, name: string, fullPage = false) {
  // Reset scroll and focus so fixed headers and the skip link do not appear
  // mid-page in viewport or full-page evidence.
  await page.waitForTimeout(250);
  await page.evaluate(() => {
    window.scrollTo(0, 0);
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
  });
  await page.screenshot({ path: path.join(assets, name), fullPage });
}

test("capture the real Reference World and two-user demo", async ({ browser }) => {
  await mkdir(assets, { recursive: true });
  const a = uniqueActor("demoA", "Mira Chen");
  const b = uniqueActor("demoB", "Kai Ito");

  const contextA = await browser.newContext({
    baseURL,
    viewport: { width: 1440, height: 900 },
    recordVideo: { dir: path.join(process.cwd(), "test-results/videos"), size: { width: 1280, height: 720 } }
  });
  const pageA = await contextA.newPage();
  const videoA = pageA.video();

  await pageA.goto("/");
  await expect(pageA.getByRole("heading", { name: /A living web of objects/ })).toBeVisible();
  await shot(pageA, "g05-landing.png", true);

  await register(pageA, a);
  await expect(pageA.getByRole("heading", { name: "Your Digital Self is live" })).toBeVisible();
  await shot(pageA, "g05-welcome.png");

  await pageA.getByRole("link", { name: "Open your World" }).click();
  await expect(pageA.getByRole("heading", { name: new RegExp(`Welcome back, ${a.displayName}`) })).toBeVisible();
  await shot(pageA, "g05-world.png");

  const collaborativeID = await createCollaborative(pageA, {
    title: "Reference World Field Notes",
    summary: "A living document two people and their AI can grow.",
    body:
      "Week one\n\n- A created the object.\n- B joined with contribute and derive rights.\n- The Personal AI proposed one body edit.\n\nWhat we learned\n\nVersion numbers make every collaboration concrete."
  });
  await expect(pageA.getByRole("heading", { name: "Reference World Field Notes" })).toBeVisible();
  await shot(pageA, "g05-collaborative.png");

  await pageA.getByRole("link", { name: "AI collaboration" }).click();
  await pageA.getByRole("button", { name: "Ask AI to propose a body edit" }).click();
  await expect(pageA.getByRole("heading", { name: "Proposals waiting for a human" })).toBeVisible();
  await pageA.getByRole("heading", { name: "Proposals waiting for a human" }).scrollIntoViewIfNeeded();
  await pageA.screenshot({ path: path.join(assets, "g05-ai-proposal.png") });

  await acceptNextDialog(pageA);
  await pageA.getByRole("button", { name: "Apply proposal" }).click();
  await expect(pageA.getByText("Proposal applied.")).toBeVisible();
  await pageA.getByRole("button", { name: "Ask AI to propose a derive" }).click();
  await expect(pageA.getByTestId("confirmation-card")).toBeVisible();
  await pageA.getByTestId("confirmation-card").scrollIntoViewIfNeeded();
  await pageA.screenshot({ path: path.join(assets, "g05-ai-confirmation.png") });
  await acceptNextDialog(pageA);
  await pageA.getByRole("button", { name: "Approve and execute" }).click();
  await expect(pageA.getByText(/Approval executed: created Fixture derived child at v1\./)).toBeVisible();
  await pageA.getByRole("link", { name: "Open created object" }).click();
  await shot(pageA, "g05-derived-child.png");
  await pageA.getByRole("link", { name: "View lineage" }).click();
  await expect(pageA.getByRole("heading", { name: "Lineage" })).toBeVisible();
  await shot(pageA, "g05-lineage.png");

  await pageA.goto(`/objects/${collaborativeID}?tab=history`);
  await expect(pageA.getByRole("heading", { name: "Version history" })).toBeVisible();
  await shot(pageA, "g05-history.png");

  const inviteLink = await createInvitation(pageA, collaborativeID, ["Read", "Contribute", "Derive"]);
  await shot(pageA, "g05-invite-create.png");

  const contextB = await browser.newContext({
    baseURL,
    viewport: { width: 1440, height: 900 },
    recordVideo: { dir: path.join(process.cwd(), "test-results/videos"), size: { width: 1280, height: 720 } }
  });
  const pageB = await contextB.newPage();
  const videoB = pageB.video();
  await pageB.goto(inviteLink);
  await expect(pageB.getByRole("heading", { name: "Invitation" })).toBeVisible();
  await shot(pageB, "g05-invite-preview.png");
  await registerFromInvite(pageB, b);
  await acceptInvitation(pageB, inviteLink);
  await expect(pageB.getByRole("heading", { name: "Reference World Field Notes" })).toBeVisible();
  await editBody(pageB, "B joined from a separate browser context and changed this body.");
  await shot(pageB, "g05-collaborative-shared.png");

  await pageA.goto(`/objects/${collaborativeID}?tab=work`);
  await pageA.getByTestId("object-refresh").click();
  await expect(pageA.getByText("B joined from a separate browser context and changed this body.")).toBeVisible();
  await shot(pageA, "g05-collaborative-updated.png");

  const pollID = await createInteractive(pageA, {
    title: "Where should the reference world grow next?",
    prompt: "Choose the next Living Object the community should explore.",
    options: ["A shared research note", "A community poll", "A lineage family tree"]
  });
  const pollLink = await createInvitation(pageA, pollID, ["Read", "Contribute", "Derive"]);
  await acceptInvitation(pageB, pollLink);
  await expect(pageB.getByRole("heading", { name: "Where should the reference world grow next?" })).toBeVisible();
  await pageB.locator(".option-button").filter({ hasText: "A community poll" }).click();
  await expect(pageB.getByText("Your vote: A community poll")).toBeVisible();
  await shot(pageB, "g05-interactive.png");

  await pageA.goto("/");
  await shot(pageA, "g05-world-populated.png");
  await pageA.goto("/ai");
  await expect(pageA.getByRole("heading", { name: "Personal AI" })).toBeVisible();
  await shot(pageA, "g05-personal-ai.png");
  await pageA.goto("/self");
  await expect(pageA.getByRole("heading", { name: "Digital Self" })).toBeVisible();
  await shot(pageA, "g05-digital-self.png");
  await pageA.goto("/settings");
  await shot(pageA, "g05-settings.png");

  // Chinese and mobile evidence from the same running build.
  const zhContext = await browser.newContext({ baseURL, viewport: { width: 1440, height: 900 } });
  const zhPage = await zhContext.newPage();
  await zhPage.goto("/");
  await zhPage.getByRole("button", { name: "中文" }).click();
  await expect(zhPage.locator("html")).toHaveAttribute("lang", "zh-CN");
  await shot(zhPage, "g05-zh-landing.png");
  await zhContext.close();

  await pageA.setViewportSize({ width: 390, height: 844 });
  await pageA.goto("/");
  await shot(pageA, "g05-mobile-world.png", true);
  await pageA.goto(`/objects/${collaborativeID}?tab=work`);
  await shot(pageA, "g05-mobile-object.png");

  await contextB.close();
  await contextA.close();
  // saveAs waits for Playwright's ffmpeg pipeline to finalize the recording.
  if (videoA) {
    await videoA.saveAs(path.join(assets, "g05-demo-a.webm"));
  }
  if (videoB) {
    await videoB.saveAs(path.join(assets, "g05-demo-b.webm"));
  }

  expect(pollID).toMatch(/[0-9a-f-]{36}/);
});
