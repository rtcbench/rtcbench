import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.RTCBENCH_SITE_URL ?? "http://127.0.0.1:18728";

export default defineConfig({
  testDir: ".",
  testMatch: "site.spec.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  workers: 2,
  use: { baseURL, trace: "retain-on-failure", screenshot: "only-on-failure" },
  projects: [
    {
      name: "desktop",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
      },
    },
    {
      name: "iphone",
      use: {
        ...devices["iPhone 16 Pro"],
        viewport: { width: 402, height: 874 },
      },
    },
    { name: "android", use: { ...devices["Pixel 5"] } },
  ],
  webServer: process.env.RTCBENCH_SITE_URL
    ? undefined
    : {
        command: "bun run serve-site.ts",
        url: baseURL,
      },
});
