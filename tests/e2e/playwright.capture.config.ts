// SPDX-License-Identifier: Apache-2.0
import { defineConfig } from "@playwright/test";
import path from "node:path";

const root = process.cwd();
const baseURL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:8080";

// Separate from the blocking acceptance config: this run produces the real
// screenshots and demo videos that ship with the Reference World docs.
export default defineConfig({
  testDir: path.join(root, "tests/e2e/capture"),
  timeout: 240_000,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL,
    viewport: { width: 1440, height: 900 },
    locale: "en-US",
    trace: "off",
    screenshot: "off",
    video: "off",
    ...(process.env.PW_CHANNEL_CHROME === "1" ? ({ channel: "chrome" } as const) : {})
  },
  outputDir: path.join(root, "test-results/capture")
});
