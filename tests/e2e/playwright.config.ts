// SPDX-License-Identifier: Apache-2.0
import { defineConfig, devices } from "@playwright/test";
import path from "node:path";

const baseURL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:8080";
const root = process.cwd();
// CI uses Playwright's bundled Chromium and Firefox. A local machine that
// cannot fetch the browser CDN may opt into branded Chrome/Firefox channels;
// the evidence must say which channels were used.
const chromeChannel =
  process.env.PW_CHANNEL_CHROME === "1" ? ({ channel: "chrome" } as const) : {};
const firefoxChannel =
  process.env.PW_CHANNEL_FIREFOX === "1" ? ({ channel: "firefox" } as const) : {};

// The reference stack is a real PostgreSQL instance and the real Go API served
// from the production web build. Playwright never intercepts business calls;
// the only route interception used is the explicit service-failure test.
export default defineConfig({
  testDir: path.join(root, "tests/e2e/specs"),
  globalSetup: path.join(root, "tests/e2e/support/global-setup.ts"),
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: Boolean(process.env.CI),
  reporter: [
    ["list"],
    ["html", { outputFolder: path.join(root, "playwright-report"), open: "never" }],
    ["junit", { outputFile: path.join(root, "test-results/junit.xml") }]
  ],
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
    locale: "en-US"
  },
  outputDir: path.join(root, "test-results"),
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"], baseURL, ...chromeChannel }
    },
    {
      name: "firefox",
      use: { ...devices["Desktop Firefox"], baseURL, ...firefoxChannel }
    },
    {
      name: "mobile-chromium",
      use: {
        ...devices["Desktop Chrome"],
        baseURL,
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true,
        ...chromeChannel
      },
      testMatch: /responsive-i18n\.spec\.ts/
    }
  ]
});
