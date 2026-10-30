// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { fillStable, register, uniqueActor } from "../support/actors";

interface WorldSnapshot {
  version: number;
  state: {
    title?: string;
    premise?: string;
    kinds?: unknown[];
    tags?: string[];
  };
}

async function snapshot(page: Page, id: string): Promise<WorldSnapshot> {
  return page.evaluate(async (objectID) => {
    const response = await fetch(`/api/v1/objects/${objectID}`, { credentials: "include" });
    return (await response.json()) as WorldSnapshot;
  }, id);
}

// One model reply can contain several compatible edits. The product must show
// one complete, inline plan, apply it in one version increment, and keep doing
// that for three consecutive natural-language edits without the user closing
// stale cards or handling a version conflict.
test("Phase 2 batch changes: three consecutive edits, one plan each", async ({ page }) => {
  test.setTimeout(240_000);
  await register(page, uniqueActor("p2batch", "Batch Owner"));
  await page.getByRole("button", { name: "Create a world" }).click();
  await fillStable(page.getByLabel("What do you want to begin?"), "Batch E2E Garden");
  await page.getByText("Advanced: start from a blank world").click();
  await page.getByRole("button", { name: "Create blank world" }).click();
  await expect(page.getByTestId("open-world-page")).toBeVisible({ timeout: 30_000 });
  const objectID = page.url().split("/worlds/")[1].split(/[?#]/)[0];

  await page.getByTestId("ai-entry").click();
  await expect(page.getByTestId("ai-panel")).toBeVisible();

  async function send(text: string): Promise<void> {
    const compose = page.locator("#dw-ai-compose");
    const sendButton = page.getByRole("button", { name: "Send", exact: true });
    await fillStable(compose, text);
    await expect(sendButton).toBeEnabled();
    const turnsBefore = await page.getByTestId("ai-turn").count();
    await sendButton.click();
    await expect(compose).toHaveValue("", { timeout: 30_000 });
    await expect(page.getByTestId("ai-turn")).toHaveCount(turnsBefore + 2, { timeout: 30_000 });
  }

  const turns = [
    {
      prompt: "院子再添两盏灯",
      names: ["屋檐灯", "石灯"],
      version: 2,
      kinds: 2
    },
    {
      prompt: "给院子加一条长椅",
      names: ["长椅", "树"],
      version: 3,
      kinds: 4
    },
    {
      prompt: "院子夜里更安静",
      names: ["萤火虫", "夜晚"],
      version: 4,
      kinds: 5
    }
  ];

  for (const turn of turns) {
    const cardsBefore = await page.getByTestId("proposal-card").count();
    await send(turn.prompt);
    await expect(page.getByTestId("proposal-card")).toHaveCount(cardsBefore + 1, { timeout: 30_000 });
    const card = page.getByTestId("proposal-card").last();
    await expect(card).toHaveAttribute("data-inline", "true");
    await expect(card).toContainText("Apply several changes");
    for (const name of turn.names) {
      await expect(card).toContainText(name);
    }
    const apply = card.getByRole("button", { name: "Confirm and apply" });
    await expect(apply).toBeVisible();
    await apply.click();
    await expect(page.getByText("Proposal applied.").last()).toBeVisible({ timeout: 30_000 });

    const applied = await snapshot(page, objectID);
    expect(applied.version).toBe(turn.version);
    expect(applied.state.kinds ?? []).toHaveLength(turn.kinds);

    await page.reload();
    await expect(page.getByTestId("open-world-page")).toBeVisible({ timeout: 30_000 });
    const persisted = await snapshot(page, objectID);
    expect(persisted.version).toBe(turn.version);
    expect(persisted.state.kinds ?? []).toHaveLength(turn.kinds);

    await page.getByTestId("ai-entry").click();
    await expect(page.getByTestId("ai-panel")).toBeVisible();
    await page.waitForTimeout(500);
  }

  const final = await snapshot(page, objectID);
  expect(final.state.title).toBe("夜风小院");
  expect(final.state.premise).toBe("夜深了，院子里只剩风和虫鸣。");
  expect(final.state.tags ?? []).toContain("夜晚");
  await expect(page.locator("[data-testid=proposal-card][data-state=pending]")).toHaveCount(0);
  await expect(page.getByText(/version conflict/i)).toHaveCount(0);
});
