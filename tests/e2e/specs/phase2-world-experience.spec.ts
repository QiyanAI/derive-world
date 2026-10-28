// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Locator, type Page } from "@playwright/test";
import { register, uniqueActor } from "../support/actors";

async function ensureOpen(details: Locator): Promise<void> {
  await details.evaluate((element) => {
    (element as HTMLDetailsElement).open = true;
  });
}

async function createFreeWorld(page: Page, title: string): Promise<string> {
  await page.goto("/");
  await page.getByRole("button", { name: "Create a beginning" }).click();
  await page.getByLabel("What do you want to begin?").fill(title);
  await page.getByRole("button", { name: "Define a free world" }).click();
  await page.getByRole("button", { name: "Let it grow" }).click();
  await expect(page.getByTestId("open-world-page")).toBeVisible();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/);
  return page.url().split("/worlds/")[1].split("?")[0];
}

async function addKind(
  page: Page,
  input: { name: string; shape: string; motion: string; fieldKey: string; fieldLabel: string }
): Promise<void> {
  const structure = page.getByTestId("open-world-structure");
  const details = structure.locator("details.dw-open-add").filter({ hasText: "Add a kind" }).first();
  await ensureOpen(details);
  await page.locator("#ow-kind-name").fill(input.name);
  await page.locator("#ow-kind-shape").selectOption(input.shape);
  await page.locator("#ow-kind-motion").selectOption(input.motion);
  const form = structure.locator("form").filter({ has: page.locator("#ow-kind-name") });
  await form.getByPlaceholder("Key").fill(input.fieldKey);
  await form.getByPlaceholder("Label").fill(input.fieldLabel);
  await form.locator("select").last().selectOption("number");
  await form.getByRole("button", { name: "Add field" }).click();
  await form.getByRole("button", { name: "Add a kind", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").first()).toContainText(input.name);
}

async function addNode(
  page: Page,
  input: { kind: string; name: string; text: string; x: number; y: number }
): Promise<void> {
  const structure = page.getByTestId("open-world-structure");
  const details = structure.locator("details.dw-open-add").filter({ hasText: "Place a thing" }).first();
  await ensureOpen(details);
  await page.locator("#ow-node-kind").selectOption({ label: input.kind });
  await page.locator("#ow-node-name").fill(input.name);
  await page.locator("#ow-node-text").fill(input.text);
  await page.locator("#ow-node-x").fill(String(input.x));
  await page.locator("#ow-node-y").fill(String(input.y));
  const form = structure.locator("form").filter({ has: page.locator("#ow-node-name") });
  await form.getByRole("button", { name: "Place a thing", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").nth(1)).toContainText(input.name);
}

async function addBinding(
  page: Page,
  kindName: string,
  input: { fieldLabel: string; target: string; inMin: string; inMax: string; outMin: string; outMax: string }
): Promise<void> {
  const look = page.locator("details.dw-open-kind-look").filter({ hasText: kindName });
  await ensureOpen(look);
  const add = look.locator(".dw-open-binding-add");
  await add.locator("select").nth(0).selectOption({ label: input.fieldLabel });
  await add.locator("select").nth(1).selectOption(input.target);
  await add.getByLabel("Input min").fill(input.inMin);
  await add.getByLabel("Input max").fill(input.inMax);
  await add.getByLabel("Output min").fill(input.outMin);
  await add.getByLabel("Output max").fill(input.outMax);
  await add.getByRole("button", { name: "Add binding" }).click();
  await expect(look.locator(".dw-open-binding-list")).toContainText(`${input.target}`);
  const save = look.getByRole("button", { name: "Save look" });
  const saved = page.waitForResponse((response) => {
    const request = response.request();
    return request.url().includes("/actions") && (request.postData() ?? "").includes("update_kind");
  });
  await save.click();
  expect((await saved).ok()).toBe(true);
}

async function addRule(
  page: Page,
  input: { trigger: string; target: string; fieldLabel: string; value: string }
): Promise<void> {
  const structure = page.getByTestId("open-world-structure");
  const details = structure.locator("details.dw-open-add").filter({ hasText: "Add a rule" }).first();
  await ensureOpen(details);
  const form = details.locator("form");
  await page.locator("#ow-rule-trigger").selectOption({ label: input.trigger });
  await page.locator("#ow-effect-target").selectOption({ label: input.target });
  await page.locator("#ow-effect-field").selectOption({ label: input.fieldLabel });
  await page.locator("#ow-effect-op").selectOption("add");
  await page.locator("#ow-effect-value").fill(input.value);
  await form.getByRole("button", { name: "Add a rule", exact: true }).click();
  await expect(structure.locator(".dw-open-structure-list").nth(3)).toContainText(input.trigger);
}

test("Phase 2 world experience: data-driven scene, appearance, interaction, persistence and derivation", async ({
  page
}) => {
  const actor = uniqueActor("p2scene", "Phase Two Scene Owner");
  await register(page, actor);
  const title = "A coast that answers";
  const worldID = await createFreeWorld(page, title);

  // The default view of a new free world is the data-driven scene, not a
  // relationship graph.
  await expect(page.getByRole("button", { name: "Scene", exact: true })).toHaveClass(/is-active/);

  // Two kinds with different forms. Neither shape exists as a fixed page.
  await addKind(page, {
    name: "Lamp",
    shape: "beacon",
    motion: "flicker",
    fieldKey: "brightness",
    fieldLabel: "Brightness"
  });
  await addKind(page, {
    name: "Water",
    shape: "wave",
    motion: "sway",
    fieldKey: "level",
    fieldLabel: "Level"
  });

  // Place both things at explicit scene positions.
  await addNode(page, { kind: "Lamp", name: "North lamp", text: "It answers the water.", x: 400, y: 340 });
  await addNode(page, { kind: "Water", name: "Tide", text: "It carries the answer.", x: 620, y: 720 });

  const scene = page.getByTestId("open-world-scene");
  await expect(scene).toBeVisible();
  const lamp = page.getByTestId("world-scene-node").filter({ hasText: "North lamp" });
  const tide = page.getByTestId("world-scene-node").filter({ hasText: "Tide" });
  await expect(lamp).toHaveAttribute("data-shape", "beacon");
  await expect(tide).toHaveAttribute("data-shape", "wave");
  await expect(lamp).toHaveAttribute("data-motion", "flicker");
  await expect(tide).toHaveAttribute("data-motion", "sway");

  // Bind fields to visual properties through the product, not source code.
  await addBinding(page, "Lamp", {
    fieldLabel: "Brightness",
    target: "glow",
    inMin: "0",
    inMax: "10",
    outMin: "0",
    outMax: "100"
  });
  await addBinding(page, "Water", {
    fieldLabel: "Level",
    target: "scale",
    inMin: "0",
    inMax: "10",
    outMin: "0.8",
    outMax: "1.6"
  });

  // Two declarative rules: touching the lamp changes itself and the water.
  await addRule(page, { trigger: "North lamp", target: "Tide", fieldLabel: "Level", value: "2" });
  await addRule(page, {
    trigger: "North lamp",
    target: "North lamp",
    fieldLabel: "Brightness",
    value: "2"
  });

  const initialTideSize = await tide.getAttribute("data-size");
  const initialLampGlow = await lamp.getAttribute("data-glow");

  // A real click runs the rules and the bound visuals change immediately.
  await lamp.click();
  await expect
    .poll(async () => tide.getAttribute("data-size"), { timeout: 15_000 })
    .not.toBe(initialTideSize);
  await expect
    .poll(async () => lamp.getAttribute("data-glow"), { timeout: 15_000 })
    .not.toBe(initialLampGlow);

  // The state is the server's, not a browser-only effect.
  await page.reload();
  await expect(page.getByTestId("open-world-scene")).toBeVisible();
  await page.getByRole("button", { name: "Cards", exact: true }).click();
  const tideCard = page.getByTestId("open-world-card").filter({ hasText: "Tide" });
  await expect(tideCard.locator('input[type="number"]')).toHaveValue("2");

  // Change the world's own presentation through the product.
  await page.getByRole("button", { name: "Scene", exact: true }).click();
  const look = page.getByTestId("open-world-look");
  await look.locator("select").first().selectOption("void");
  await look.locator("form").getByRole("button", { name: "Save look" }).click();
  await expect(page.getByTestId("open-world-scene")).toHaveAttribute("data-backdrop", "void");
  await page.reload();
  await expect(page.getByTestId("open-world-scene")).toHaveAttribute("data-backdrop", "void");

  // Derive the whole experience through the real derive path.
  await page.getByRole("link", { name: "Branch", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/objects/${worldID}/derive`));
  await page.getByLabel("Title of the new branch").fill("A coast with another answer");
  await page.getByRole("button", { name: "Create derived object" }).click();
  await expect(page).toHaveURL(/\/objects\/[0-9a-f-]{36}\?derived=1/);
  const childID = page.url().split("/objects/")[1].split("?")[0];
  expect(childID).not.toBe(worldID);

  await page.goto(`/worlds/${childID}`);
  await expect(page.getByTestId("open-world-scene")).toHaveAttribute("data-backdrop", "void");
  await expect(page.getByTestId("world-scene-node").filter({ hasText: "North lamp" })).toHaveAttribute(
    "data-shape",
    "beacon"
  );

  // Change only the child's look; the parent must remain exactly as it was.
  const childLook = page.getByTestId("open-world-look");
  await childLook.locator("select").first().selectOption("horizon");
  await childLook.locator("form").getByRole("button", { name: "Save look" }).click();
  await expect(page.getByTestId("open-world-scene")).toHaveAttribute("data-backdrop", "horizon");

  const states = await page.evaluate(
    async ({ parentID, childID }) => {
      const read = async (id: string) => {
        const response = await fetch(`/api/v1/objects/${id}`, { credentials: "include" });
        return (await response.json()) as {
          state: { presentation: { scene: { backdrop: string } } };
        };
      };
      const parent = await read(parentID);
      const child = await read(childID);
      const members = await fetch(`/api/v1/objects/${childID}/members`, { credentials: "include" }).then(
        (response) => response.json()
      );
      const publications = await fetch(`/api/v1/objects/${childID}/publications`, {
        credentials: "include"
      }).then((response) => response.json());
      return {
        parentBackdrop: parent.state.presentation.scene.backdrop,
        childBackdrop: child.state.presentation.scene.backdrop,
        memberCount: Array.isArray(members.items) ? members.items.length : -1,
        publicationCount: Array.isArray(publications.items) ? publications.items.length : -1
      };
    },
    { parentID: worldID, childID }
  );
  expect(states.parentBackdrop).toBe("void");
  expect(states.childBackdrop).toBe("horizon");
  expect(states.memberCount).toBe(1);
  expect(states.publicationCount).toBe(0);
});
