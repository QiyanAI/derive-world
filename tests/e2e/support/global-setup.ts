// SPDX-License-Identifier: Apache-2.0
import { request } from "@playwright/test";

const baseURL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:8080";

// global-setup proves the suite is attached to the real fixture stack before
// any browser context is created. It fails closed on model mode, Origin
// mismatch, or a missing server.
export default async function globalSetup(): Promise<void> {
  const context = await request.newContext({ baseURL });
  try {
    let ready = false;
    for (let attempt = 0; attempt < 60; attempt += 1) {
      try {
        const response = await context.get("/readyz", { timeout: 2_000 });
        if (response.ok()) {
          ready = true;
          break;
        }
      } catch {
        // The stack may still be starting; retry until the deadline.
      }
      await new Promise((resolve) => setTimeout(resolve, 1_000));
    }
    if (!ready) {
      throw new Error(`Derive World is not ready at ${baseURL}`);
    }

    const metaResponse = await context.get("/api/v1/meta");
    if (!metaResponse.ok()) {
      throw new Error(`/api/v1/meta returned ${metaResponse.status()}`);
    }
    const meta = (await metaResponse.json()) as { model_mode?: string };
    if (
      (process.env.CI || process.env.PW_REQUIRE_FIXTURE === "1") &&
      meta.model_mode !== "fixture"
    ) {
      throw new Error(
        `Browser acceptance requires fixture mode, got ${String(meta.model_mode)}`
      );
    }

    // Login is an unsafe cookie endpoint: a 403 here means PUBLIC_BASE_URL and
    // E2E_BASE_URL disagree, which would otherwise surface as a confusing
    // failure in the first registration test.
    const probe = await context.post("/api/v1/auth/login", {
      headers: {
        Origin: baseURL,
        "Content-Type": "application/json"
      },
      data: {
        email: "origin.probe@example.test",
        password: "not-a-real-password"
      }
    });
    if (probe.status() === 403) {
      throw new Error(
        `Origin/CSRF rejected ${baseURL}; set PUBLIC_BASE_URL to the same value`
      );
    }
    if (probe.status() !== 401) {
      throw new Error(`Unexpected login probe status ${probe.status()}`);
    }
  } finally {
    await context.dispose();
  }
}
