import { defineConfig } from "@playwright/test";

const webBaseURL = process.env.PLAYWRIGHT_WEB_BASE_URL || "http://127.0.0.1:4173";
const apiBaseURL = process.env.PLAYWRIGHT_API_BASE_URL || "http://127.0.0.1:8999";

export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.e2e.ts",
  timeout: 120_000,
  expect: {
    timeout: 15_000,
  },
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: webBaseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
    },
  ],
  webServer: [
    {
      command: `cd .. && exec go run ./test/e2e/hostedapi -addr ${apiBaseURL.replace("http://", "")}`,
      url: `${apiBaseURL}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      command:
        `cd .. && cd web && bun run build && cd .. ` +
        `&& exec go run ./test/e2e/webserver -addr ${webBaseURL.replace("http://", "")} -dir ./web/dist -api ${apiBaseURL}`,
      url: webBaseURL,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      stdout: "pipe",
      stderr: "pipe",
    },
  ],
});
