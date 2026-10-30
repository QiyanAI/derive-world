// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { register, uniqueActor } from "../support/actors";

async function api(
  page: Page,
  path: string,
  method: string,
  body?: Record<string, unknown>
): Promise<{ status: number; body: any }> {
  return page.evaluate(
    async ({ path, method, body }) => {
      const csrf = document.cookie
        .split("; ")
        .find((entry) => entry.startsWith("dw_csrf="))
        ?.split("=")[1];
      const idempotency = Array.from({ length: 40 }, () =>
        "abcdefghijklmnopqrstuvwxyz0123456789"[Math.floor(Math.random() * 36)]
      ).join("");
      const response = await fetch(path, {
        method,
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": idempotency,
          ...(csrf ? { "X-CSRF-Token": csrf } : {})
        },
        body: body === undefined ? undefined : JSON.stringify(body)
      });
      const text = await response.text();
      return { status: response.status, body: text ? JSON.parse(text) : null };
    },
    { path, method, body }
  );
}

test("Phase 2C renderer: every presentation module renders and changes real state", async ({ page }) => {
  const actor = uniqueActor("p2render", "Phase Two Renderer Owner");
  await register(page, actor);

  const created = await api(page, "/api/v1/objects", "POST", {
    object_type: "open_world",
    title: "Renderer world",
    content: {
      premise: "Two things and one score.",
      kinds: [
        {
          name: "Item",
          hue: 190,
          fields: [
            { key: "score", label: "Score", type: "number" },
            { key: "done", label: "Done", type: "flag" }
          ]
        }
      ],
      nodes: [
        { kind_index: 0, name: "Alpha", text: "", x: 400, y: 400, values: { score: 1, done: false } },
        { kind_index: 0, name: "Beta", text: "", x: 900, y: 500, values: { score: 2, done: false } }
      ],
      links: [],
      rules: [],
      views: ["scene", "cards"]
    },
    shared_context: {}
  });
  expect(created.status).toBe(201);
  const state = created.body.state as {
    kinds: Array<{ id: string }>;
    nodes: Array<{ id: string; name: string }>;
  };
  const kindID = state.kinds[0]!.id;
  const alphaID = state.nodes.find((node) => node.name === "Alpha")!.id;
  const betaID = state.nodes.find((node) => node.name === "Beta")!.id;
  const worldID = created.body.id as string;

  const experience = {
    protocol: 1,
    root: {
      id: "root",
      capability: "core.stack",
      props: { direction: "vertical", gap: 12, padding: 12, align: "stretch", tone: "panel" },
      children: [
        {
          id: "title",
          capability: "core.text",
          props: { text: "{{world.title}}", size: 24, weight: "bold", tone: "default", align: "left" }
        },
        {
          id: "space",
          capability: "core.space",
          props: {
            background: { mode: "gradient", hue: 222, hue2: 205, lightness: 10, accent_hue: 42 },
            show_links: true,
            link_style: "line",
            views: [{
              kind_id: kindID,
              path: "M 0 -30 L 30 0 L 0 30 L -30 0 Z",
              size: 72,
              fill_hue: 190,
              opacity: 0.9,
              paint: "fill",
              label: "always",
              layer: "mid",
              bindings: [{ field: "score", target: "size", in_min: 0, in_max: 10, out_min: 0.8, out_max: 1.6 }]
            }],
            activate_behavior_id: "bump-space"
          }
        },
        {
          id: "list",
          capability: "core.collection",
          props: { kind_id: kindID, layout: "list", columns: 1, empty_text: "" },
          children: [{
            id: "card",
            capability: "core.stack",
            props: { direction: "vertical", gap: 6, padding: 8, align: "stretch", tone: "subtle" },
            children: [
              {
                id: "name",
                capability: "core.text",
                props: { text: "{{item.name}} · {{item.field.score}}", size: 17, weight: "regular", tone: "default", align: "left" }
              },
              {
                id: "meter",
                capability: "core.meter",
                props: { entity_id: "$item", field: "score", label: "Score", min: 0, max: 10, tone: "accent" }
              },
              {
                id: "field",
                capability: "core.field",
                props: { entity_id: "$item", field: "done", label: "Done", control: "flag" }
              },
              {
                id: "bump",
                capability: "core.button",
                props: { label: "Bump item", behavior_id: "bump-item", tone: "ghost" }
              }
            ]
          }]
        },
        {
          id: "choice",
          capability: "core.choice",
          props: {
            prompt: "What next?",
            options: [
              { label: "Add one", behavior_id: "bump-a" },
              { label: "Clear Alpha", behavior_id: "clear-a" }
            ]
          }
        }
      ]
    },
    behaviors: [
      {
        id: "bump-space",
        actions: [{ capability: "core.state", action: "add", params: { entity_id: alphaID, field: "score", value: 1 } }]
      },
      {
        id: "bump-item",
        actions: [{ capability: "core.state", action: "add", params: { entity_id: "$event.entity", field: "score", value: 1 } }]
      },
      {
        id: "bump-a",
        actions: [{ capability: "core.state", action: "add", params: { entity_id: alphaID, field: "score", value: 1 } }]
      },
      {
        id: "clear-a",
        actions: [{ capability: "core.state", action: "set", params: { entity_id: alphaID, field: "score", value: 0 } }]
      }
    ]
  };
  const saved = await api(page, `/api/v1/objects/${worldID}/actions`, "POST", {
    action: "set_experience",
    input: { experience },
    expected_version: 1
  });
  expect(saved.status).toBe(200);

  await page.goto(`/worlds/${worldID}`);
  await expect(page.getByTestId("world-experience")).toBeVisible();
  await expect(page.getByTestId("world-experience-space")).toBeVisible();
  await expect(page.getByTestId("world-experience-collection")).toBeVisible();
  await expect(page.getByTestId("world-experience-meter")).toHaveCount(2);
  await expect(page.getByTestId("world-experience-field")).toHaveCount(2);
  await expect(page.getByTestId("world-experience-choice")).toBeVisible();

  await expect(page.getByTestId("world-experience-meter").first()).toHaveAttribute("data-value", "1");
  await page.getByTestId("world-experience-space").getByTestId("world-experience-entity").first().click();
  await expect(page.getByTestId("world-experience-meter").first()).toHaveAttribute("data-value", "2");
  await page.getByTestId("world-experience-button").first().click();
  await expect(page.getByTestId("world-experience-meter").first()).toHaveAttribute("data-value", "3");
  await page.getByTestId("world-experience-field").first().getByRole("button").click();
  await expect(page.getByTestId("world-experience-field").first().getByRole("button")).toHaveAttribute("aria-pressed", "true");
  await page.getByTestId("world-experience-choice-option").filter({ hasText: "Clear Alpha" }).click();
  await expect(page.getByTestId("world-experience-meter").first()).toHaveAttribute("data-value", "0");
  await page.reload();
  await expect(page.getByTestId("world-experience-meter").first()).toHaveAttribute("data-value", "0");
  await expect(page.getByTestId("world-experience-field").first().getByRole("button")).toHaveAttribute("aria-pressed", "true");
});
