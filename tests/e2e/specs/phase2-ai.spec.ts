// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { register, uniqueActor } from "../support/actors";

async function stageCount(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const response = await fetch("/api/v1/world/stage", { credentials: "include" });
    const data = (await response.json()) as { objects: unknown[] };
    return data.objects.length;
  });
}

test("Phase 2 AI: a zero-world user creates a real free world through human approval", async ({ page }) => {
  const actor = uniqueActor("p2ai", "Phase Two AI Owner");
  await register(page, actor);
  await expect(page.getByTestId("world-onboarding")).toBeVisible();
  expect(await stageCount(page)).toBe(0);

  // The Personal AI is available before the first world and is not the
  // object-bound inspector.
  await page.getByTestId("personal-ai-entry").click();
  const dialog = page.getByTestId("personal-ai-dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("General mode · no world context");
  await expect(dialog).toContainText("Fixture");

  await dialog.getByRole("button", { name: "Create my first world" }).click();
  const proposal = dialog.getByTestId("world-proposal");
  await expect(proposal).toBeVisible({ timeout: 30_000 });
  await expect(proposal).toContainText("A city that changes with every choice");
  await expect(dialog).toContainText("Nothing is created yet");

  // The proposal is not a creation: the real backend still has zero worlds.
  expect(await stageCount(page)).toBe(0);

  await proposal.getByRole("button", { name: "Create this world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByTestId("open-world-page")).toBeVisible();
  await expect(page.getByTestId("open-world-node")).toHaveCount(3);

  const objectID = page.url().split("/worlds/")[1].split("?")[0];
  const created = await page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/objects/${id}`, { credentials: "include" });
    return (await response.json()) as {
      object_type: string;
      state: { nodes: Array<{ name: string }>; rules: unknown[] };
    };
  }, objectID);
  expect(created.object_type).toBe("open_world");
  expect(created.state.nodes).toHaveLength(3);
  expect(created.state.nodes[0]?.name).toBe("The first resident");
  expect(created.state.rules.length).toBeGreaterThan(0);

  // Leaving the world keeps the same Personal AI available in general mode.
  await page.getByRole("link", { name: "Back to my worlds" }).click();
  await expect(page.getByTestId("open-world-page")).toHaveCount(0);
  await expect(page.getByTestId("dw-stage-node")).toHaveCount(1);
  await page.getByTestId("personal-ai-entry").click();
  await expect(page.getByTestId("personal-ai-dialog")).toContainText("General mode · no world context");
});

test("Phase 2 AI: world context is explicit and switching back to general keeps identity", async ({
  page
}) => {
  const actor = uniqueActor("p2context", "Phase Two Context Owner");
  await register(page, actor);

  // Create a living scene through the ordinary V2 flow.
  await page.goto("/");
  await page.getByRole("button", { name: "Create a beginning" }).click();
  await page.getByLabel("What do you want to begin?").fill("A context world");
  await page.getByRole("button", { name: "Let it grow" }).click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();
  await inspector.getByRole("tab", { name: "AI", exact: true }).click();
  await expect(inspector.getByTestId("ai-world-context")).toContainText("Using the context of “A context world”");
  await inspector.locator("textarea").fill("world-only hello");
  await inspector.getByRole("button", { name: "Send", exact: true }).click();
  await expect(inspector.getByText("world-only hello")).toBeVisible();

  // The world panel names the same user-level AI and offers a deliberate
  // switch to the general conversation.
  await inspector.getByRole("button", { name: "Switch to general AI" }).click();
  const dialog = page.getByTestId("personal-ai-dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("General mode · no world context");
  await dialog.locator("#dw-personal-ai-draft").fill("general-only hello");
  await dialog.getByRole("button", { name: "Send", exact: true }).click();
  await expect(dialog.getByText("general-only hello")).toBeVisible();

  // The general conversation is unbound; the world's conversation is bound to
  // that object. They are separate private records with no cross-reading.
  const conversations = await page.evaluate(async () => {
    const response = await fetch("/api/v1/ai/conversations", { credentials: "include" });
    return (await response.json()) as {
      items: Array<{ id: string; object_id: string | null }>;
    };
  });
  const bound = conversations.items.find((item) => item.object_id !== null);
  const general = conversations.items.find((item) => item.object_id === null);
  expect(bound).toBeTruthy();
  expect(general).toBeTruthy();
  const details = await page.evaluate(async (ids) => {
    const out: Record<string, string> = {};
    for (const id of ids) {
      const response = await fetch(`/api/v1/ai/conversations/${id}`, { credentials: "include" });
      const data = (await response.json()) as { messages: Array<{ content: string }> };
      out[id] = data.messages.map((message) => message.content).join("\n");
    }
    return out;
  }, [bound?.id ?? "", general?.id ?? ""]);
  expect(details[bound?.id ?? ""]).toContain("world-only hello");
  expect(details[bound?.id ?? ""]).not.toContain("general-only hello");
  expect(details[general?.id ?? ""]).toContain("general-only hello");
  expect(details[general?.id ?? ""]).not.toContain("world-only hello");
});
