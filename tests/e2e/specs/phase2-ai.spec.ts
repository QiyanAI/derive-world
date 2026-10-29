// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { register, uniqueActor, fillStable } from "../support/actors";

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
  await expect(dialog).toContainText("Creation mode · you approve every world");
  await expect(dialog).toContainText("Fixture");

  await dialog.getByRole("button", { name: "Create my first world" }).click();
  const proposal = dialog.getByTestId("world-proposal");
  await expect(proposal).toBeVisible({ timeout: 30_000 });
  await expect(proposal).toContainText("A city that changes with every choice");
  await expect(proposal).toContainText("nothing is written until you confirm");

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
  await expect(page.getByTestId("personal-ai-dialog")).toContainText("Creation mode · you approve every world");
});

test("Phase 2 AI: world context is explicit and switching back to general keeps identity", async ({
  page
}) => {
  const actor = uniqueActor("p2context", "Phase Two Context Owner");
  await register(page, actor);

  // Create a living scene through the ordinary V2 flow.
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "A context world");
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Idea scene" }).click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  const inspector = page.getByTestId("stage-inspector");
  await expect(inspector).toBeVisible();
  await inspector.getByRole("tab", { name: "AI", exact: true }).click();
  await expect(inspector.getByTestId("ai-world-context")).toContainText("Using the context of “A context world”");
  await inspector.locator("textarea").fill("world-only hello");
  await inspector.getByRole("button", { name: "Send", exact: true }).click();
  // Wait for the persisted assistant turn, not just the composer text. The
  // fixture provider answers immediately; navigating before the response would
  // abort the request and leave a bound conversation with no messages.
  await expect(
    inspector.getByText("Fixture reply: no scripted response", { exact: false })
  ).toBeVisible({ timeout: 30_000 });

  // The world panel has no escape into a context-free conversation. The only
  // general AI entry lives outside a world, on Home.
  await expect(inspector.getByRole("button", { name: "Switch to general AI" })).toHaveCount(0);
  await page.goto("/");
  await page.getByTestId("personal-ai-entry").click();
  const dialog = page.getByTestId("personal-ai-dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Creation mode · you approve every world");
  await dialog.locator("#dw-personal-ai-draft").fill("general-only hello");
  await dialog.getByRole("button", { name: "Send", exact: true }).click();
  await expect(dialog.getByText("general-only hello")).toBeVisible();

  // The general conversation is unbound; the world's conversation is bound to
  // that object. They are separate private records with no cross-reading.
  // The send response is persisted before the message renders; poll the list
  // so a slow list read cannot race the just-created conversation row.
  await expect
    .poll(async () => {
      const items = await page.evaluate(async () => {
        const response = await fetch("/api/v1/ai/conversations", { credentials: "include" });
        return ((await response.json()) as {
          items: Array<{ id: string; object_id: string | null }>;
        }).items;
      });
      return {
        bound: items.some((item) => item.object_id !== null),
        general: items.some((item) => item.object_id === null)
      };
    }, { timeout: 15_000 })
    .toEqual({ bound: true, general: true });
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

  // A delayed list read or a second conversation row must not hide the
  // private record that actually holds the message. Read every conversation
  // and assert both directions of isolation, rather than trusting the first
  // row returned for each binding.
  await expect
    .poll(async () => {
      return page.evaluate(async () => {
        const listResponse = await fetch("/api/v1/ai/conversations", { credentials: "include" });
        const list = (await listResponse.json()) as {
          items: Array<{ id: string; object_id: string | null }>;
        };
        const details: Array<{ object_id: string | null; text: string }> = [];
        for (const item of list.items) {
          const response = await fetch(`/api/v1/ai/conversations/${item.id}`, {
            credentials: "include"
          });
          const data = (await response.json()) as { messages: Array<{ content: string }> };
          details.push({
            object_id: item.object_id,
            text: data.messages.map((message) => message.content).join("\n")
          });
        }
        return {
          boundHasWorld: details.some(
            (entry) => entry.object_id !== null && entry.text.includes("world-only hello")
          ),
          generalHasGeneral: details.some(
            (entry) => entry.object_id === null && entry.text.includes("general-only hello")
          ),
          crossRead: details.some(
            (entry) =>
              (entry.object_id !== null && entry.text.includes("general-only hello")) ||
              (entry.object_id === null && entry.text.includes("world-only hello"))
          )
        };
      });
    }, { timeout: 15_000 })
    .toEqual({ boundHasWorld: true, generalHasGeneral: true, crossRead: false });
});
