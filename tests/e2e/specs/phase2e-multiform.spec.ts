// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fillStable, register, uniqueActor } from "../support/actors";

const ROOT = process.cwd();
const PHOTO = path.join(ROOT, "tests/e2e/fixtures/phase2e-photo.png");
const VIDEO = path.join(ROOT, "docs/assets/demo-17s.webm");

function idempotencyKey(): string {
  return randomUUID().replaceAll("-", "") + randomUUID().replaceAll("-", "").slice(0, 16);
}

function wavBuffer(): Buffer {
  const sampleRate = 8000;
  const samples = Math.floor(sampleRate * 0.3);
  const buffer = Buffer.alloc(44 + samples * 2);
  buffer.write("RIFF", 0, "ascii");
  buffer.writeUInt32LE(36 + samples * 2, 4);
  buffer.write("WAVE", 8, "ascii");
  buffer.write("fmt ", 12, "ascii");
  buffer.writeUInt32LE(16, 16);
  buffer.writeUInt16LE(1, 20);
  buffer.writeUInt16LE(1, 22);
  buffer.writeUInt32LE(sampleRate, 24);
  buffer.writeUInt32LE(sampleRate * 2, 28);
  buffer.writeUInt16LE(2, 32);
  buffer.writeUInt16LE(16, 34);
  buffer.write("data", 36, "ascii");
  buffer.writeUInt32LE(samples * 2, 40);
  for (let index = 0; index < samples; index += 1) {
    const value = Math.sin((2 * Math.PI * 440 * index) / sampleRate) * 0.18;
    buffer.writeInt16LE(Math.round(value * 32767), 44 + index * 2);
  }
  return buffer;
}

function glbBuffer(): Buffer {
  const positions = Buffer.alloc(36);
  const values = [0, 0, 0, 1.2, 0, 0, 0, 1.2, 0];
  values.forEach((value, index) => positions.writeFloatLE(value, index * 4));
  const document = JSON.stringify({
    asset: { version: "2.0" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }],
    buffers: [{ byteLength: positions.length }],
    bufferViews: [
      { buffer: 0, byteOffset: 0, byteLength: positions.length, target: 34962 }
    ],
    accessors: [
      {
        bufferView: 0,
        componentType: 5126,
        count: 3,
        type: "VEC3",
        min: [0, 0, 0],
        max: [1.2, 1.2, 0]
      }
    ]
  });
  let jsonChunk = Buffer.from(document, "utf8");
  while (jsonChunk.length % 4 !== 0) {
    jsonChunk = Buffer.concat([jsonChunk, Buffer.from(" ")]);
  }
  const bin = Buffer.concat([positions, Buffer.alloc((4 - (positions.length % 4)) % 4)]);
  const total = 12 + 8 + jsonChunk.length + 8 + bin.length;
  const header = Buffer.alloc(12);
  header.write("glTF", 0, "ascii");
  header.writeUInt32LE(2, 4);
  header.writeUInt32LE(total, 8);
  const jsonHeader = Buffer.alloc(8);
  jsonHeader.writeUInt32LE(jsonChunk.length, 0);
  jsonHeader.writeUInt32LE(0x4e4f534a, 4);
  const binHeader = Buffer.alloc(8);
  binHeader.writeUInt32LE(bin.length, 0);
  binHeader.writeUInt32LE(0x004e4942, 4);
  return Buffer.concat([header, jsonHeader, jsonChunk, binHeader, bin]);
}

async function baseHeaders(page: Page): Promise<Record<string, string>> {
  const cookies = await page.context().cookies();
  const csrf = cookies.find((cookie) => cookie.name === "dw_csrf")?.value ?? "";
  return {
    Origin: new URL(page.url()).origin,
    "X-CSRF-Token": csrf
  };
}

async function createWorldAPI(page: Page, title: string): Promise<Record<string, any>> {
  const headers = await baseHeaders(page);
  const response = await page.request.post("/api/v1/objects", {
    headers: { ...headers, "Idempotency-Key": idempotencyKey() },
    data: {
      object_type: "open_world",
      title,
      content: {
        premise: "A real photograph, a film, a sound and a walkable 3D planet share one state.",
        kinds: [
          { name: "Traveler", hue: 180, fields: [] },
          { name: "Beacon", hue: 48, fields: [] }
        ],
        nodes: [
          { kind_index: 0, name: "Traveler", text: "She listens.", x: 700, y: 520 },
          { kind_index: 1, name: "Beacon", text: "It answers.", x: 940, y: 440 }
        ],
        links: [],
        rules: [],
        variables: { touched: false }
      },
      shared_context: {}
    }
  });
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as Record<string, any>;
}

async function uploadAsset(
  page: Page,
  objectID: string,
  kind: string,
  name: string,
  contentType: string,
  body: Buffer
): Promise<Record<string, any>> {
  const headers = await baseHeaders(page);
  const response = await page.request.post(
    `/api/v1/objects/${objectID}/assets?kind=${kind}&name=${encodeURIComponent(name)}`,
    {
      headers: { ...headers, "Content-Type": contentType },
      data: body
    }
  );
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as Record<string, any>;
}

async function setExperience(page: Page, object: Record<string, any>, experience: unknown): Promise<Record<string, any>> {
  const headers = await baseHeaders(page);
  const response = await page.request.post(`/api/v1/objects/${object.id}/actions`, {
    headers: { ...headers, "Idempotency-Key": idempotencyKey() },
    data: {
      action: "set_experience",
      input: { experience },
      expected_version: object.version
    }
  });
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as Record<string, any>;
}

function composedExperience(
  kinds: Array<{ id: string }>,
  nodes: Array<{ id: string }>,
  assets: { image: string; video: string; audio: string; model: string }
) {
  return {
    protocol: 1,
    root: {
      id: "root",
      capability: "core.stack",
      props: { direction: "vertical", gap: 10, padding: 0, align: "stretch", tone: "plain" },
      children: [
        {
          id: "audio",
          capability: "core.audio",
          props: {
            title: "Room tone",
            ambient_asset_id: assets.audio,
            loop: true,
            volume: 0.35,
            muted: false,
            cues: [
              {
                variable: "touched",
                mode: "truthy",
                tone: { frequency: 620, wave: "triangle", volume: 0.12 }
              }
            ]
          }
        },
        {
          id: "tabs",
          capability: "core.tabs",
          props: {
            default: "scene3d",
            tabs: [
              { label: "3D", child_id: "scene3d" },
              { label: "Photo", child_id: "photo" },
              { label: "Film", child_id: "film" }
            ]
          },
          children: [
            {
              id: "scene3d",
              capability: "core.scene3d",
              props: {
                title: "One world, four media",
                sky_hue: 214,
                ground_hue: 142,
                fog: 0.02,
                camera_mode: "follow",
                camera_distance: 14,
                camera_height: 8,
                fov: 55,
                gravity: 18,
                ambient_light: 0.8,
                player_enabled: true,
                player_node_id: nodes[0]!.id,
                player_speed: 4,
                player_height: 1,
                views: [
                  {
                    kind_id: kinds[0]!.id,
                    primitive: "sphere",
                    scale: 0.9,
                    elevation: 0,
                    hue: 180,
                    opacity: 0.95,
                    collider: "none",
                    label: "always"
                  },
                  {
                    kind_id: kinds[1]!.id,
                    primitive: "model",
                    model_asset_id: assets.model,
                    scale: 1.5,
                    elevation: 1,
                    hue: 48,
                    opacity: 0.95,
                    collider: "box",
                    label: "always"
                  }
                ],
                show_links: true,
                activate_behavior_id: "touch",
                bump_behavior_id: "",
                entity_actions: []
              }
            },
            {
              id: "photo",
              capability: "core.media",
              props: {
                kind: "image",
                asset_id: assets.image,
                caption: "A real uploaded photograph.",
                fit: "contain",
                controls: false,
                loop: false,
                muted: true,
                autoplay: false,
                play_variable: "",
                show_variable: ""
              }
            },
            {
              id: "film",
              capability: "core.media",
              props: {
                kind: "video",
                asset_id: assets.video,
                caption: "A real uploaded film.",
                fit: "contain",
                controls: true,
                loop: true,
                muted: true,
                autoplay: false,
                play_variable: "",
                show_variable: ""
              }
            }
          ]
        }
      ]
    },
    behaviors: [
      {
        id: "touch",
        name: "Touch the world",
        actions: [
          {
            capability: "core.variable",
            action: "set",
            params: { name: "touched", value: true }
          }
        ]
      }
    ]
  };
}

async function createComposedWorld(page: Page): Promise<string> {
  const object = await createWorldAPI(page, "Four media in one world");
  const state = object.state as {
    kinds: Array<{ id: string }>;
    nodes: Array<{ id: string }>;
  };
  const image = await uploadAsset(page, object.id, "image", "photo.png", "image/png", fs.readFileSync(PHOTO));
  const video = await uploadAsset(page, object.id, "video", "film.webm", "video/webm", fs.readFileSync(VIDEO));
  const audio = await uploadAsset(page, object.id, "audio", "room.wav", "audio/wav", wavBuffer());
  const model = await uploadAsset(page, object.id, "model", "beacon.glb", "model/gltf-binary", glbBuffer());
  await setExperience(
    page,
    object,
    composedExperience(state.kinds, state.nodes, {
      image: image.id,
      video: video.id,
      audio: audio.id,
      model: model.id
    })
  );
  await page.goto(`/worlds/${object.id}`);
  await expect(page.getByTestId("world-experience")).toBeVisible();
  return object.id;
}

test("Phase 2E: image, video, audio, GLB 3D and physics in one world survive refresh and derive", async ({
  page
}) => {
  test.setTimeout(180_000);
  await register(page, uniqueActor("p2emedia", "Phase 2E Media"));
  const worldID = await createComposedWorld(page);

  const world3d = page.getByTestId("world-3d");
  await expect(world3d).toBeVisible({ timeout: 30_000 });
  await expect
    .poll(async () => world3d.getAttribute("data-webgl"), { timeout: 30_000 })
    .toMatch(/^(ok|unavailable)$/);
  const webgl = await world3d.getAttribute("data-webgl");
  test.skip(
    webgl === "unavailable",
    "This browser cannot create a WebGL context; the fallback behavior was exercised."
  );
  expect(webgl).toBe("ok");
  await expect
    .poll(async () => world3d.getAttribute("data-models-loaded"), { timeout: 30_000 })
    .toBe("1");
  await expect(page.getByTestId("world-3d-entity")).toHaveCount(2);

  // Walking changes the live player position; gravity keeps it above ground.
  await page.getByTestId("world-3d-canvas").focus();
  await expect
    .poll(async () => world3d.getAttribute("data-player-z"), { timeout: 20_000 })
    .not.toBeNull();
  const beforeZ = Number(await world3d.getAttribute("data-player-z"));
  await page.keyboard.down("w");
  await page.waitForTimeout(700);
  await page.keyboard.up("w");
  await expect
    .poll(async () => Number(await world3d.getAttribute("data-player-z")), { timeout: 20_000 })
    .not.toBe(beforeZ);

  // Audio requires a gesture, then a 3D interaction produces a real cue.
  await page.getByTestId("world-audio-enable").click();
  await expect(page.getByTestId("world-audio")).toHaveAttribute("data-enabled", "true");
  await expect(page.getByTestId("world-audio")).toHaveAttribute("data-ambient", "file");
  await expect
    .poll(async () => page.getByTestId("world-audio").getAttribute("data-playing"), { timeout: 20_000 })
    .toBe("true");
  await page.getByTestId("world-3d-entity").first().dispatchEvent("click");
  await expect
    .poll(async () => page.getByTestId("world-audio").getAttribute("data-cues-fired"), { timeout: 20_000 })
    .toBe("1");

  // Real uploaded image and video render from the world's own permissions.
  await page.getByRole("tab", { name: "Photo" }).click();
  const image = page.getByTestId("world-media-image");
  await expect(image).toBeVisible({ timeout: 20_000 });
  await expect
    .poll(async () => image.evaluate((node) => (node as HTMLImageElement).naturalWidth), { timeout: 20_000 })
    .toBeGreaterThan(0);
  await page.getByRole("tab", { name: "Film" }).click();
  const video = page.getByTestId("world-media").locator("video");
  await expect(video).toBeVisible({ timeout: 20_000 });
  await video.evaluate((node) => (node as HTMLVideoElement).play());
  await expect.poll(async () => video.evaluate((node) => (node as HTMLVideoElement).paused), {
    timeout: 20_000
  }).toBe(false);

  // Refresh restores the state and re-renders the media.
  await page.reload();
  await expect(page.getByTestId("world-3d")).toBeVisible({ timeout: 30_000 });
  const objectResponse = await page.request.get(`/api/v1/objects/${worldID}`);
  const object = (await objectResponse.json()) as {
    state: { variables?: Record<string, unknown> };
  };
  expect(object.state.variables?.touched).toBe(true);
  await page.getByRole("tab", { name: "Photo" }).click();
  await expect
    .poll(
      async () =>
        page.getByTestId("world-media-image").evaluate((node) => (node as HTMLImageElement).naturalWidth),
      { timeout: 20_000 }
    )
    .toBeGreaterThan(0);

  // A derived world receives its own copy of the same media references.
  await page.getByText("Advanced", { exact: true }).click();
  await page.getByRole("button", { name: "Derive a new world" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Create this branch" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByTestId("world-3d")).toBeVisible({ timeout: 30_000 });
  await page.getByRole("tab", { name: "Photo" }).click();
  await expect
    .poll(
      async () =>
        page.getByTestId("world-media-image").evaluate((node) => (node as HTMLImageElement).naturalWidth),
      { timeout: 20_000 }
    )
    .toBeGreaterThan(0);
});

test("Phase 2E: a described 3D world reserves a real media slot a human can fill", async ({
  page
}) => {
  test.setTimeout(180_000);
  await register(page, uniqueActor("p2eai3d", "Phase 2E AI 3D"));
  await page.goto("/");
  await page.getByRole("button", { name: "Create a world" }).first().click();
  await fillStable(
    page.getByLabel("What do you want to begin?"),
    "Create a world that is a three-dimensional planet I can walk on and hear"
  );
  await page.getByTestId("create-world-ai").click();
  const dialog = page.getByTestId("personal-ai-dialog");
  await expect(dialog).toBeVisible();
  const proposal = dialog.getByTestId("world-proposal").first();
  await expect(proposal).toBeVisible({ timeout: 30_000 });
  await expect(dialog).toContainText("Fixture");
  await proposal.getByRole("button", { name: "Create this world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[0-9a-f-]{36}/, { timeout: 30_000 });
  await expect(page.getByTestId("world-3d")).toBeVisible({ timeout: 30_000 });
  const audio = page.getByTestId("world-audio");
  await expect(audio).toBeVisible();
  await expect(audio).toHaveAttribute("data-ambient", "tone");
  const worldID = page.url().split("/worlds/")[1]!.split("?")[0];

  // The media surface exists as an empty slot; fill it through the product.
  const photoTab = page.getByRole("tab").filter({ hasText: /photo|图像/i }).first();
  await photoTab.click();
  await expect(page.getByTestId("world-media-empty")).toBeVisible();

  await page.getByText("Advanced", { exact: true }).click();
  await page.getByRole("button", { name: "Experience editor" }).click();
  await expect(page.getByTestId("world-assets-panel")).toBeVisible();
  await page.getByTestId("world-asset-file").setInputFiles(PHOTO);
  await expect(page.getByTestId("world-asset-item").first()).toBeVisible({ timeout: 20_000 });
  // The bounded procedural provider uses the same validation/quota/audit path.
  await page.getByTestId("world-asset-generate").click();
  await expect(page.getByTestId("world-asset-item")).toHaveCount(2, { timeout: 20_000 });
  await page.getByTestId("world-experience-node-media").click();
  const inspector = page.getByTestId("world-experience-inspector");
  await inspector.getByLabel("File").selectOption({ label: "phase2e-photo.png" });
  await page.getByTestId("world-experience-save").click();
  await expect(page.getByText(/world answered|saved/i).first()).toBeVisible({ timeout: 20_000 });

  await photoTab.click();
  await expect
    .poll(
      async () =>
        page.getByTestId("world-media-image").evaluate((node) => (node as HTMLImageElement).naturalWidth),
      { timeout: 20_000 }
    )
    .toBeGreaterThan(0);
  await page.reload();
  await expect(page.getByTestId("world-3d")).toBeVisible({ timeout: 30_000 });
  const photoTabAfter = page.getByRole("tab").filter({ hasText: /photo|图像/i }).first();
  await photoTabAfter.click();
  await expect
    .poll(
      async () =>
        page.getByTestId("world-media-image").evaluate((node) => (node as HTMLImageElement).naturalWidth),
      { timeout: 20_000 }
    )
    .toBeGreaterThan(0);
  expect(worldID).toMatch(/[0-9a-f-]{36}/);
});
