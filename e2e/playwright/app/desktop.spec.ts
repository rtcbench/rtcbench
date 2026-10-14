import type { Page } from "@playwright/test";
import { test, expect } from "./fixtures";

test.use({ screen: { width: 1920, height: 1080 } });

const fakeWebView = (page: Page) =>
  page.addInitScript(() => {
    const sent: string[] = [];
    Object.assign(window, {
      sent,
      chrome: {
        webview: { postMessage: (message: string) => sent.push(message) },
      },
    });
  });
const sent = (page: Page) =>
  page.evaluate(() => (window as unknown as { sent: string[] }).sent.splice(0));
const stripPoint = async (page: Page) => {
  const strip = (await page
    .locator(".lab-toolbar-tabs")
    .first()
    .boundingBox())!;
  return { x: strip.x + strip.width - 8, y: strip.y + strip.height / 2 };
};

test("Windows puts its caption buttons flush in the corner", async ({
  page,
}) => {
  await fakeWebView(page);
  await page.goto("welcome?desktop=windows");
  await expect(page.getByRole("tab").first()).toBeVisible();
  expect(await sent(page)).toEqual(["WALT"]);

  const viewport = page.viewportSize()!;
  const close = page.getByRole("button", { name: "Close", exact: true });
  const box = (await close.boundingBox())!;
  expect(box.x + box.width).toBe(viewport.width);
  expect(box.y).toBe(0);
  const strip = (await page.locator(".lab-toolbar").first().boundingBox())!;
  expect(box.height).toBeGreaterThanOrEqual(strip.y + strip.height - 1);
  const panelTop = await page.evaluate(
    () =>
      document
        .querySelector(".lab-panel:not([hidden])")!
        .getBoundingClientRect().top,
  );
  expect(box.y + box.height).toBeLessThanOrEqual(panelTop);
  const lastTab = (await page.getByRole("tab").last().boundingBox())!;
  const minimize = (await page
    .getByRole("button", { name: "Minimize", exact: true })
    .boundingBox())!;
  expect(minimize.x).toBeGreaterThan(lastTab.x + lastTab.width);

  const { x, y } = await stripPoint(page);
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 40, y + 10);
  await page.mouse.up();
  expect(await sent(page)).toEqual(["drag"]);
  await page.mouse.dblclick(x, y);
  expect(await sent(page)).toContain("Wt");

  await page.mouse.move(2, 400);
  await page.mouse.down();
  await page.mouse.up();
  expect(await sent(page)).toEqual(["resize:w-resize"]);

  for (const [name, message] of [
    ["Minimize", "Wm"],
    ["Maximize", "Wt"],
    ["Close", "Q"],
  ]) {
    await page.getByRole("button", { name, exact: true }).click();
    expect(await sent(page)).toEqual([message]);
  }

  await page.getByRole("button", { name: "RTCBench", exact: true }).click();
  await page.getByRole("button", { name: "Toggle theme" }).click();
  expect(await sent(page)).toEqual(["WADT"]);
  await page.keyboard.press("Escape");

  await page.setViewportSize({ width: 2400, height: 1000 });
  await expect(
    page.getByRole("separator", { name: /Resize workspace/ }),
  ).toHaveCount(0);
  const workspace = (await page
    .getByRole("region", { name: "Workspace" })
    .boundingBox())!;
  expect(2400 - workspace.x - workspace.width).toBeLessThan(24);
});

test("macOS drags by the tab strip and keeps its menu clear of the traffic lights", async ({
  page,
}) => {
  await fakeWebView(page);
  await page.goto("welcome?desktop=darwin");
  await expect(page.getByRole("tab").first()).toBeVisible();
  await sent(page);
  const { x, y } = await stripPoint(page);
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 40, y + 10);
  await page.mouse.up();
  expect(await sent(page)).toEqual(["drag"]);
  await page.mouse.dblclick(x, y);
  expect(await sent(page)).toContain("Wt");

  const tab = (await page.getByRole("tab").first().boundingBox())!;
  await page.mouse.move(tab.x + tab.width / 2, tab.y + tab.height / 2);
  await page.mouse.down();
  await page.mouse.up();
  expect(await sent(page)).toEqual([]);

  const menu = page.getByRole("button", { name: "RTCBench", exact: true });
  const lastTab = (await page.getByRole("tab").last().boundingBox())!;
  expect((await menu.boundingBox())!.x).toBeGreaterThan(
    lastTab.x + lastTab.width,
  );
  await menu.click();
  await expect(
    page.getByRole("dialog", { name: "RTCBench menu" }),
  ).toBeInViewport({ ratio: 1 });
  await page.keyboard.press("Escape");

  const browser = await page.context().newPage();
  await browser.goto("welcome");
  await expect(browser.getByRole("tab").first()).toBeVisible();
  expect(
    (await browser
      .getByRole("button", { name: "RTCBench", exact: true })
      .boundingBox())!.x,
  ).toBeLessThan(100);
});

for (const [os, name] of [
  ["darwin", "RTCBench"],
  ["windows", "Close"],
])
  test(`the ${os} corner control stays put while tabs are dragged into a split`, async ({
    page,
  }) => {
    await fakeWebView(page);
    await page.goto(`welcome?desktop=${os}`);
    const control = page.getByRole("button", { name, exact: true });
    const corner = async () => {
      const box = (await control.boundingBox())!;
      return { x: Math.round(box.x), y: Math.round(box.y) };
    };
    const start = await corner();
    const tab = (await page
      .getByRole("tab", { name: "Configs", exact: true })
      .boundingBox())!;
    const viewport = page.viewportSize()!;
    await page.mouse.move(tab.x + tab.width / 2, tab.y + tab.height / 2);
    await page.mouse.down();
    await page.mouse.move(viewport.width - 120, viewport.height / 2, {
      steps: 20,
    });
    await page.waitForTimeout(400);
    expect(await corner()).toEqual(start);
    await page.mouse.up();
    await expect(page.locator("[data-pane-header]")).toHaveCount(2);
    await page.waitForTimeout(400);
    expect(await corner()).toEqual(start);
    if (os !== "windows") return;
    const plus = (await page
      .getByRole("button", { name: "Open a tab", exact: true })
      .last()
      .boundingBox())!;
    const minimize = (await page
      .getByRole("button", { name: "Minimize", exact: true })
      .boundingBox())!;
    expect(plus.x + plus.width).toBeLessThanOrEqual(minimize.x);
  });

for (const [os, shortcut] of [
  ["windows", "F11"],
  ["darwin", "Control+Meta+f"],
])
  test(`${os} toggles full screen from the keyboard and the menu`, async ({
    page,
  }) => {
    await fakeWebView(page);
    await page.goto(`welcome?desktop=${os}`);
    await expect(page.getByRole("tab").first()).toBeVisible();
    await sent(page);
    await page.keyboard.press(shortcut);
    expect(await sent(page)).toEqual(["WF"]);
    await page.getByRole("button", { name: "RTCBench", exact: true }).click();
    await page
      .getByRole("button", { name: "Full screen", exact: true })
      .click();
    expect(await sent(page)).toEqual(["WF"]);
  });

test("the browser app leaves F11 to the browser", async ({ page }) => {
  await fakeWebView(page);
  await page.goto("welcome");
  await expect(page.getByRole("tab").first()).toBeVisible();
  await page.keyboard.press("F11");
  expect(await sent(page)).toEqual([]);
  await page.getByRole("button", { name: "RTCBench", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Full screen", exact: true }),
  ).toBeVisible();
});

test("pressing a key after clicking the content does not outline it", async ({
  page,
}) => {
  await fakeWebView(page);
  await page.goto("runs?desktop=windows");
  const panel = page.locator(".lab-panel:not([hidden])").first();
  await panel.click({ position: { x: 400, y: 300 } });
  await page.keyboard.press("F11");
  await expect(panel).toBeFocused();
  await expect(panel).toHaveCSS("box-shadow", "none");

  await page.getByRole("tab", { name: "Runs", exact: true }).first().focus();
  for (let i = 0; i < 20; i++) {
    if (await panel.evaluate((el) => el === document.activeElement)) break;
    await page.keyboard.press("Tab");
  }
  await expect(panel).toBeFocused();
  await expect(panel).not.toHaveCSS("box-shadow", "none");
});
