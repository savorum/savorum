import { defineConfig, devices } from "@playwright/test"

/** The browser tests drive the real Savorum binary; there is no mock server. */
const port = 8123

export default defineConfig({
  testDir: "./e2e",
  timeout: 90_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  reporter: "line",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    ...devices["Desktop Chrome"],
    // Downloading the generated document is part of what is under test.
    acceptDownloads: true,
    // Cached responses would carry one test's state into the next; the offline
    // cache is exercised by the tests that ask for the worker.
    serviceWorkers: "block",
    // Keep successful runs artifact-free while retaining bounded diagnostics
    // for the failed-job artifact, which CI keeps for one day.
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "off",
  },
  webServer: {
    command: `node e2e/serve.mjs ${port}`,
    url: `http://127.0.0.1:${port}/api/v1/health`,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: "pipe",
    stderr: "pipe",
  },
})
