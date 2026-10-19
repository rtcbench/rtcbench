import { expect, test, type Page } from "@playwright/test";

const release = {
  tag_name: "v0.17.5",
  assets: [
    "rtcbench-headless-linux-amd64.tar.gz",
    "rtcbench-headless-linux-arm64.tar.gz",
    "rtcbench-windows-amd64.exe",
    "rtcbench-macos-arm64.zip",
  ].map((name) => ({
    name,
    size: 22_000_000,
    browser_download_url: `https://example.test/${name}`,
  })),
};

test.beforeEach(async ({ page }) => {
  await page.route("**/releases.json", (route) =>
    route.fulfill({ json: [release] }),
  );
  await page.route("**/checksums/v0.17.5.txt", (route) =>
    route.fulfill({
      body: `${"a".repeat(64)}  rtcbench-headless-linux-amd64.tar.gz\n`,
    }),
  );
  await page.goto("/welcome");
  await expect(page.getByRole("link", { name: /^Download for/ })).toContainText(
    "v0.17.5",
  );
});

async function fits(page: Page) {
  const panel = page.getByRole("tabpanel");
  expect(await panel.evaluate((el) => el.scrollWidth - el.clientWidth)).toBe(0);
  expect(
    await panel.evaluate((el) => {
      el.scrollLeft = 1000;
      return el.scrollLeft;
    }),
  ).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBe(0);
  const bounds = await panel.boundingBox();
  for (const control of [
    page.getByRole("link", { name: "Star on GitHub" }),
    page.getByRole("link", { name: "Come say hi!" }),
    page.getByRole("link", { name: /^Download for/ }),
    page.getByRole("button", { name: "Choose a platform" }),
  ]) {
    const box = await control.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(bounds!.x + 8);
    expect(box!.x + box!.width).toBeLessThanOrEqual(
      bounds!.x + bounds!.width - 8,
    );
    expect(box!.height).toBeGreaterThanOrEqual(44);
  }
}

test("welcome controls fit without sideways content scrolling", async ({
  page,
}) => {
  await fits(page);
  await page
    .getByRole("button", { name: "Show download command and hash" })
    .click();
  await fits(page);
  await page
    .getByRole("button", { name: "RTCBench: A Scalable", exact: false })
    .click();
  await fits(page);
  await page.setViewportSize({ width: 320, height: 740 });
  await fits(page);
});

test("phones default to Linux x64 Headless", async ({ page, isMobile }) => {
  test.skip(!isMobile, "Mobile fallback");
  const download = page.getByRole("link", { name: /^Download for/ });
  await expect(download).toContainText("Download for Linux x64");
  await expect(download).toContainText("Headless");
  await expect(download).toContainText("22 MB");
  await expect(download).toHaveAttribute(
    "href",
    "https://example.test/rtcbench-headless-linux-amd64.tar.gz",
  );
  await expect(page.getByRole("switch", { name: "Headless" })).toBeChecked();
  await page
    .getByRole("button", { name: "Show download command and hash" })
    .click();
  await expect(page.getByText("a".repeat(64), { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Choose a platform" }).click();
  await page.getByRole("menuitemradio", { name: /^Windows x64/ }).click();
  await expect(download).toHaveAttribute(
    "href",
    "https://example.test/rtcbench-windows-amd64.exe",
  );
  await expect(
    page.getByRole("switch", { name: "Headless" }),
  ).not.toBeChecked();
  await fits(page);
});

test("chart labels stay readable and inside the chart when resized", async ({
  page,
}) => {
  const chart = page.getByRole("img", { name: /^LiveKit receivers/ });
  for (const width of [402, 320, 874, 1440]) {
    await page.setViewportSize({ width, height: 874 });
    await expect
      .poll(() =>
        chart.evaluate((el) =>
          Math.abs(
            el.getBoundingClientRect().width -
              (el as SVGSVGElement).viewBox.baseVal.width,
          ),
        ),
      )
      .toBeLessThan(1);
    await expect
      .poll(async () => {
        const labels = await chart.locator("text").evaluateAll((elements) =>
          elements.map((el) => {
            const { left, right, top, bottom } = el.getBoundingClientRect();
            const svg = el.closest("svg")!;
            const bounds = svg.getBoundingClientRect();
            return {
              rect: { left, right, top, bottom },
              fontSize:
                parseFloat(getComputedStyle(el).fontSize) *
                svg.getScreenCTM()!.a,
              inside:
                left >= bounds.left &&
                right <= bounds.right + 1 &&
                top >= bounds.top &&
                bottom <= bounds.bottom + 1,
            };
          }),
        );
        return labels.every(
          (label, index) =>
            label.fontSize >= 12 &&
            label.inside &&
            labels
              .slice(index + 1)
              .every(
                (other) =>
                  label.rect.right <= other.rect.left ||
                  label.rect.left >= other.rect.right ||
                  label.rect.bottom <= other.rect.top ||
                  label.rect.top >= other.rect.bottom,
              ),
        );
      })
      .toBe(true);
    await expect(chart).toContainText("2,512");
  }
});
