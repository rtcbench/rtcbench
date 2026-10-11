import { defineConfig } from "@playwright/test";

const remote = process.env.RTCBENCH_BASE_URL;
const baseURL = (remote ?? "http://127.0.0.1:18727/app").replace(/\/?$/, "/");

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 2,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL,
    browserName: "chromium",
    channel: process.env.PLAYWRIGHT_CHANNEL,
    viewport: { width: 1440, height: 1000 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: process.env.RTCBENCH_RECORD ? "on" : "retain-on-failure",
    storageState: process.env.RTCBENCH_STORAGE_STATE,
    extraHTTPHeaders: { Origin: new URL(baseURL).origin },
    httpCredentials: process.env.RTCBENCH_HTTP_USER
      ? {
          username: process.env.RTCBENCH_HTTP_USER,
          password: process.env.RTCBENCH_HTTP_PASSWORD ?? "",
        }
      : undefined,
  },
  webServer: remote
    ? undefined
    : {
        command: "bun run serve.ts",
        url: baseURL + "api/v1/version",
        timeout: 30_000,
      },
});
