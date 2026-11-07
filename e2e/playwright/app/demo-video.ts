import { chromium } from "playwright";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";

const base = process.env.RTCBENCH_URL ?? "http://127.0.0.1:18726/";
const config = process.env.DEMO_CONFIG ?? "livekit-demo";
const name = process.env.DEMO_RUN ?? config;
const dir = process.env.DEMO_DIR ?? "demo-video";
const seconds = Number(process.env.DEMO_SECONDS ?? 15);
const size = { width: 1440, height: 900 };

await mkdir(dir, { recursive: true });
const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: size,
  colorScheme: "dark",
  recordVideo: { dir, size },
});
const page = await context.newPage();
const started = Date.now();
const until = (ms: number) =>
  page.waitForTimeout(Math.max(0, started + ms - Date.now()));

await page.goto(base + "runs");
const newRun = page.getByRole("button", { name: "New run", exact: true });
await newRun.waitFor();
await until(700);
await newRun.click();
await page
  .getByRole("textbox", { name: "Name", exact: true })
  .pressSequentially(name, { delay: 45 });
await page.getByRole("combobox", { name: "Config", exact: true }).click();
await until(2800);
await page.getByRole("option", { name: config, exact: true }).click();
await until(3600);
await page.getByRole("button", { name: "Start", exact: true }).click();
await page.getByRole("heading", { name, exact: true }).waitFor();
const id = page.url().match(/run\/([^/?#]+)/)?.[1];

const plot = page
  .locator('[data-slot="timeline-chart"] [role="slider"]')
  .first();
await plot.waitFor();
await until(8500);
const box = await plot.boundingBox();
if (box) {
  const steps = 48;
  for (let i = 0; i <= steps; i++) {
    await page.mouse.move(
      box.x + (box.width * i) / steps,
      box.y + box.height / 2,
    );
    await page.waitForTimeout(45);
  }
}
await until(11600);
await page
  .getByRole("heading", { name: /^Viewers/ })
  .evaluate((heading) =>
    heading.scrollIntoView({ behavior: "smooth", block: "start" }),
  );
await until(seconds * 1000);

const video = page.video()!;
await context.close();
const target = join(dir, "charts-demo.webm");
await video.saveAs(target);
await video.delete();
await browser.close();
if (id) await fetch(`${base}api/v1/runs/${id}/stop`, { method: "POST" });
console.log(target);
