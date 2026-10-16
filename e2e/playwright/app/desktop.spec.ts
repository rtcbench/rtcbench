import type { Page } from "@playwright/test";
import { test, expect, tabAction } from "./fixtures";

test.use({ screen: { width: 1920, height: 1080 } });

const fakeWebView = (page: Page, os = "windows") =>
  page.addInitScript((os) => {
    const sent: string[] = [];
    const host = { postMessage: (message: string) => sent.push(message) };
    Object.defineProperties(screen, {
      width: { value: 1920 },
      height: { value: 1080 },
    });
    Object.assign(window, {
      sent,
      ...(os === "windows"
        ? { chrome: { webview: host } }
        : { webkit: { messageHandlers: { external: host } } }),
    });
  }, os);
const sent = (page: Page) =>
  page.evaluate(() => (window as unknown as { sent: string[] }).sent.splice(0));
const stripPoint = async (page: Page) => {
  const strip = (await page
    .locator(".lab-toolbar-tabs")
    .first()
    .boundingBox())!;
  return { x: strip.x + strip.width - 8, y: strip.y + strip.height / 2 };
};

for (const os of ["windows", "linux"])
  test(`${os} puts its caption buttons flush in the corner`, async ({
    page,
  }) => {
    await fakeWebView(page, os);
    await page.goto(`welcome?desktop=${os}`);
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

    for (const [edge, x, y] of [
      ["w", 2, 400],
      ["e", viewport.width - 2, 400],
      ["n", 500, 2],
      ["s", 500, viewport.height - 2],
      ["nw", 2, 2],
      ["ne", viewport.width - 2, 2],
      ["sw", 2, viewport.height - 2],
      ["se", viewport.width - 2, viewport.height - 2],
    ] as const) {
      await page.mouse.move(x, y);
      await page.mouse.down();
      await page.mouse.move(500, 400);
      await page.mouse.up();
      expect(await sent(page)).toEqual([`resize:${edge}-resize`]);
    }

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
  await fakeWebView(page, "darwin");
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
  ["linux", "Close"],
])
  test(`the ${os} corner control stays put while tabs are dragged into a split`, async ({
    page,
  }) => {
    await fakeWebView(page, os);
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
    if (os === "darwin") return;
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
  ["linux", "F11"],
  ["darwin", "Control+Meta+f"],
])
  test(`${os} toggles full screen from the keyboard and the menu`, async ({
    page,
  }) => {
    const actions = async () =>
      (await sent(page)).filter((message) => !message.startsWith("EE"));
    await fakeWebView(page, os);
    await page.goto(`welcome?desktop=${os}`);
    await expect(page.getByRole("tab").first()).toBeVisible();
    await sent(page);
    await page.keyboard.press(shortcut);
    expect(await actions()).toEqual(["WF"]);
    await page.setViewportSize({ width: 1920, height: 1080 });
    await expect(
      page.locator(".lab-window-controls, .lab-traffic-lights"),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "RTCBench", exact: true }).click();
    await page
      .getByRole("button", { name: "Full screen", exact: true })
      .click();
    expect(await actions()).toEqual(["Wf"]);
    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 1440, height: 1000 });
    await expect(
      page.locator(".lab-window-controls, .lab-traffic-lights"),
    ).toHaveCount(1);
    await sent(page);
    await page.keyboard.press(shortcut);
    expect(await actions()).toEqual(["WF"]);
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.keyboard.press(shortcut);
    expect(await actions()).toEqual(["Wf"]);
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

for (const os of ["darwin", "windows", "linux"])
  test(`${os} title bar aligns across densities, themes, splits and reloads`, async ({
    page,
  }) => {
    await fakeWebView(page, os);
    await page.goto(`welcome?desktop=${os}`);
    const aligned = async () => {
      const toolbar = page.locator(".lab-toolbar").first();
      if (os === "darwin") {
        await expect(page.locator(".lab-window-controls")).toHaveCount(0);
        await expect
          .poll(async () => {
            const bottom = await toolbar.evaluate(
              (el) => el.getBoundingClientRect().bottom,
            );
            const center = await page.evaluate(() => {
              const message = (window as unknown as { sent: string[] }).sent
                .filter((value) =>
                  value.startsWith('EE{"name":"traffic-lights"'),
                )
                .at(-1);
              return message
                ? (JSON.parse(message.slice(2)).data[0] as number)
                : -1;
            });
            return Math.abs(center - bottom / 2);
          })
          .toBeLessThan(0.5);
        const spacer = (await page
          .locator(".lab-traffic-lights")
          .boundingBox())!;
        const tab = (await page.getByRole("tab").first().boundingBox())!;
        expect(tab.x).toBeGreaterThanOrEqual(spacer.x + spacer.width);
      } else {
        const close = (await page
          .getByRole("button", { name: "Close", exact: true })
          .boundingBox())!;
        expect(close.x + close.width).toBe(page.viewportSize()!.width);
        expect(close.y).toBe(0);
        const bottom = await toolbar.evaluate(
          (el) => el.getBoundingClientRect().bottom,
        );
        expect(close.height).toBeGreaterThanOrEqual(bottom - 1);
      }
    };
    const menu = page.getByRole("button", { name: "RTCBench", exact: true });
    for (const value of [0, 40, 100]) {
      await menu.click();
      await page
        .getByRole("button", { name: "Workspace size", exact: true })
        .click();
      const slider = page.getByRole("slider", {
        name: "Workspace size",
        exact: true,
      });
      await slider.fill(String(value));
      await expect(slider).toHaveValue(String(value));
      await page.keyboard.press("Escape");
      await expect(slider).toBeHidden();
      await page.keyboard.press("Escape");
      await expect(
        page.getByRole("dialog", { name: "RTCBench menu" }),
      ).toBeHidden();
      await aligned();
    }
    await menu.click();
    await page
      .getByRole("button", { name: "Toggle theme", exact: true })
      .click();
    await expect(page.locator("html")).toHaveClass(/dark/);
    await page.keyboard.press("Escape");
    await aligned();
    await tabAction(
      page.getByRole("tab", { name: "Configs", exact: true }),
      "Split right",
    );
    await expect(page.locator("[data-pane-header]")).toHaveCount(2);
    await aligned();
    await page.getByRole("tab", { name: "Runs", exact: true }).click();
    await expect(page).not.toHaveURL(/desktop=/);
    await page.reload();
    await expect(page.locator("[data-pane-header]")).toHaveCount(2);
    await expect(page.locator("html")).toHaveClass(/dark/);
    await aligned();
    await menu.click();
    await page
      .getByRole("button", { name: "Workspace size", exact: true })
      .click();
    await expect(
      page.getByRole("slider", { name: "Workspace size", exact: true }),
    ).toHaveValue("100");
  });

for (const os of ["darwin", "windows", "linux"])
  test(`a copied ${os} URL and saved desktop marker keep ordinary browser behavior`, async ({
    page,
  }) => {
    await page.addInitScript(
      (os) => sessionStorage.setItem("rtcbench:desktop", os),
      os,
    );
    await page.goto(`welcome?desktop=${os}`);
    await expect(page.getByRole("tab").first()).toBeVisible();
    await expect(
      page.locator(".lab-window-controls, .lab-traffic-lights"),
    ).toHaveCount(0);
    expect(
      (await page
        .getByRole("button", { name: "RTCBench", exact: true })
        .boundingBox())!.x,
    ).toBeLessThan(100);
    await page.getByRole("tab", { name: "Runs", exact: true }).click();
    await page.reload();
    await expect(page.getByRole("tab").first()).toBeVisible();
    await expect(
      page.locator(".lab-window-controls, .lab-traffic-lights"),
    ).toHaveCount(0);
  });

test("the ordinary browser menu uses the browser Fullscreen API", async ({
  page,
}) => {
  await page.goto("welcome");
  const menu = page.getByRole("button", { name: "RTCBench", exact: true });
  await menu.click();
  const fullscreen = page.getByRole("button", {
    name: "Full screen",
    exact: true,
  });
  await fullscreen.click();
  await expect
    .poll(() => page.evaluate(() => !!document.fullscreenElement))
    .toBe(true);
  await expect(fullscreen).toHaveAttribute("aria-pressed", "true");
  await fullscreen.click();
  await expect
    .poll(() => page.evaluate(() => !!document.fullscreenElement))
    .toBe(false);
});
