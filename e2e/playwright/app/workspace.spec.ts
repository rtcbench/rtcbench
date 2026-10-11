import { test, expect, tabAction, drag, expectContained } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.goto("welcome");
  await expect(
    page.getByRole("tab", { name: "Welcome", exact: true }),
  ).toBeVisible();
});

test("new-tab menu opens groups on demand and allows duplicate pages", async ({
  page,
}) => {
  await page
    .locator("[data-pane-header]")
    .getByRole("button", { name: "Open a tab", exact: true })
    .click();
  await expect(page.getByRole("menu")).toHaveCount(1);
  await expect(page.getByRole("menuitemcheckbox")).toHaveCount(0);
  await page.getByRole("menuitem", { name: "Runs", exact: true }).click();
  await expect(
    page.getByRole("tab", { name: "Runs", exact: true }),
  ).toHaveCount(2);
  await page
    .locator("[data-pane-header]")
    .getByRole("button", { name: "Open a tab", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "Systems", exact: true }).hover();
  await page.getByRole("menuitem", { name: "Networks", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Networks", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/app\/systems\/networks$/);
});

test("keyboard switching, reordering, duplicating and closing keeps focus", async ({
  page,
}) => {
  const welcome = page.getByRole("tab", { name: "Welcome", exact: true });
  await welcome.focus();
  await page.keyboard.press("End");
  await expect(
    page.getByRole("tab", { name: "Configs", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Alt+ArrowLeft");
  await expect(page.getByRole("tab")).toHaveText([
    "Welcome",
    "Configs",
    "Runs",
  ]);
  await tabAction(welcome, "Duplicate tab");
  await expect(
    page.getByRole("tab", { name: "Welcome", exact: true }),
  ).toHaveCount(2);
  await expect(
    page.getByRole("tab", { name: "Welcome", exact: true }).last(),
  ).toBeFocused();
  await page.keyboard.press("Delete");
  await expect(
    page.getByRole("tab", { name: "Welcome", exact: true }),
  ).toHaveCount(1);
  await expect(page.getByRole("tab", { selected: true })).toBeFocused();
});

test("nested splits resize by keyboard and restore after reload", async ({
  page,
}) => {
  await tabAction(
    page.getByRole("tab", { name: "Configs", exact: true }),
    "Split right",
  );
  await tabAction(
    page.getByRole("tab", { name: "Runs", exact: true }),
    "Split below",
  );
  await expect(page.getByRole("tabpanel")).toHaveCount(3);
  const separator = page.getByRole("separator").first();
  const before = Number(await separator.getAttribute("aria-valuenow"));
  await separator.focus();
  await page.keyboard.press("ArrowRight");
  await expect
    .poll(async () => Number(await separator.getAttribute("aria-valuenow")))
    .toBeGreaterThan(before);
  await page.reload();
  await expect(page.getByRole("tabpanel")).toHaveCount(3);
  for (const viewport of [
    { width: 820, height: 650 },
    { width: 1920, height: 1080 },
    { width: 1100, height: 740 },
  ]) {
    await page.setViewportSize(viewport);
    await expectContained(page);
  }
  await page.getByRole("tab", { name: "Welcome", exact: true }).focus();
  await page.keyboard.press("F6");
  await expect(
    page.getByRole("tab", { name: "Welcome", exact: true }),
  ).not.toBeFocused();
});

for (const transition of ["default", "slow split"]) {
  test(`dragging between tab groups moves the tab and its content (${transition})`, async ({
    page,
  }) => {
    if (transition === "slow split")
      await page.addStyleTag({
        content:
          "[data-pane-header] { transition-duration: 1200ms !important; }",
      });
    await tabAction(
      page.getByRole("tab", { name: "Configs", exact: true }),
      "Split right",
    );
    const target = await page
      .getByRole("tab", { name: "Configs", exact: true })
      .boundingBox();
    await drag(page, page.getByRole("tab", { name: "Runs", exact: true }), {
      x: target!.x + target!.width * 0.8,
      y: target!.y + target!.height / 2,
    });
    const group = page
      .getByRole("tablist")
      .filter({ has: page.getByRole("tab", { name: "Configs", exact: true }) });
    await expect(
      group.getByRole("tab", { name: "Runs", exact: true }),
    ).toHaveCount(1);
    await expect(
      page.getByRole("tabpanel", { name: "Runs", exact: true }),
    ).toBeVisible();
    await expect(
      page.locator("[data-dragged-tab], [data-drag-content]"),
    ).toHaveCount(0);
    await expectContained(page);
  });
}

test("overflow tabs remain reachable and extreme drags stay contained", async ({
  page,
}) => {
  test.slow();
  for (let i = 0; i < 12; i++) {
    await page
      .locator("[data-pane-header]")
      .getByRole("button", { name: "Open a tab", exact: true })
      .click();
    await page.getByRole("menuitem", { name: "Runs", exact: true }).click();
  }
  await page.setViewportSize({ width: 820, height: 650 });
  const tabs = page.getByRole("tab");
  await tabs.last().focus();
  await page.keyboard.press("Home");
  await expect(tabs.first()).toBeFocused();
  await expect(tabs.first()).toBeInViewport();
  await page.keyboard.press("End");
  await expect(tabs.last()).toBeFocused();
  await expect(tabs.last()).toBeInViewport();
  const box = await tabs.last().boundingBox();
  await drag(page, tabs.last(), { x: 819, y: box!.y + box!.height / 2 });
  await expect(tabs).toHaveCount(15);
  await expect(page.locator("[data-dragged-tab]")).toHaveCount(0);
  await expectContained(page);
});

test("theme stays open and continuous density resizes nested panes", async ({
  page,
}) => {
  await tabAction(
    page.getByRole("tab", { name: "Configs", exact: true }),
    "Split right",
  );
  await page.getByRole("button", { name: "RTCBench", exact: true }).click();
  const theme = page.getByRole("button", { name: "Toggle theme", exact: true });
  await theme.click();
  await expect(theme).toBeVisible();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page
    .getByRole("button", { name: "Workspace size", exact: true })
    .click();
  const slider = page.getByRole("slider", {
    name: "Workspace size",
    exact: true,
  });
  await slider.focus();
  await page.keyboard.press("Home");
  await expect(slider).toHaveValue("0");
  const compact = (await page.getByRole("tab").first().boundingBox())!.height;
  for (let i = 0; i < 20; i++) await page.keyboard.press("ArrowUp");
  await page.keyboard.press("End");
  await expect(slider).toHaveValue("100");
  await expect
    .poll(
      async () => (await page.getByRole("tab").first().boundingBox())!.height,
    )
    .toBeGreaterThan(compact);
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
  await page.reload();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await expectContained(page);
});

test("continuous pane resizing and wide-screen expansion keep stable geometry", async ({
  page,
}) => {
  await page.setViewportSize({ width: 2400, height: 1200 });
  await tabAction(
    page.getByRole("tab", { name: "Configs", exact: true }),
    "Split right",
  );
  const outer = page.getByRole("separator", {
    name: "Resize workspace from right",
    exact: true,
  });
  await outer.dblclick();
  await expect(outer).toHaveAttribute("aria-valuetext", "Full width");
  const divider = page
    .getByRole("separator")
    .and(page.locator('[aria-label="Resize columns"]'));
  const box = await divider.boundingBox();
  for (const x of [box!.x - 250, box!.x + 250, box!.x]) {
    await drag(page, divider, { x, y: box!.y + box!.height / 2 });
    await expectContained(page);
  }
  const geometry = () =>
    page.getByRole("tabpanel").evaluateAll((panels) =>
      panels.map((panel) => {
        const b = panel.getBoundingClientRect();
        return [b.x, b.y, b.width, b.height].map(Math.round);
      }),
    );
  await expect
    .poll(async () => {
      const before = await geometry();
      await page.evaluate(
        () =>
          new Promise((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(resolve)),
          ),
      );
      return JSON.stringify(before) === JSON.stringify(await geometry());
    })
    .toBe(true);
  await page.reload();
  await expect(outer).toHaveAttribute("aria-valuetext", "Full width");
  await expectContained(page);
});

test("Escape cancels a lifted tab without changing pane membership", async ({
  page,
}) => {
  await tabAction(
    page.getByRole("tab", { name: "Configs", exact: true }),
    "Split right",
  );
  const runs = page.getByRole("tab", { name: "Runs", exact: true });
  await runs.click();
  const tab = await runs.boundingBox();
  await page.mouse.move(tab!.x + 50, tab!.y + 15);
  await page.mouse.down();
  await page.mouse.move(1000, 400, { steps: 20 });
  await expect(page.locator("[data-dragged-tab]")).toBeVisible();
  await expect(page.locator("[data-drag-content]")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.mouse.up();
  await expect(
    page.locator("[data-dragged-tab], [data-drag-content]"),
  ).toHaveCount(0);
  const original = page
    .getByRole("tablist")
    .filter({ has: page.getByRole("tab", { name: "Welcome", exact: true }) });
  await expect(
    original.getByRole("tab", { name: "Runs", exact: true }),
  ).toHaveCount(1);
  await expect(page.getByRole("tabpanel")).toHaveCount(2);
  await expectContained(page);
});
