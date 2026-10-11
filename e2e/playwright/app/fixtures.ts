import {
  test as base,
  expect,
  type Locator,
  type Page,
} from "@playwright/test";

export const test = base.extend<{ healthyPage: void }>({
  healthyPage: [
    async ({ page }, use) => {
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await use();
      expect(errors, "Uncaught browser errors").toEqual([]);
    },
    { auto: true },
  ],
});
export { expect };

export const uniqueName = (prefix: string) =>
  `e2e-${prefix}-${crypto.randomUUID().slice(0, 8)}`;

export async function select(page: Page, label: string, value: string) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: value, exact: true }).click();
}

export async function tabAction(tab: Locator, action: string) {
  await tab.click({ button: "right" });
  await tab.page().getByRole("menuitem", { name: action, exact: true }).click();
}

export async function drag(
  page: Page,
  from: Locator,
  to: { x: number; y: number },
) {
  const box = await from.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x, to.y, { steps: 20 });
  await page.mouse.up();
}

export async function expectContained(page: Page) {
  await expect
    .poll(() =>
      page.evaluate(() => {
        const root = document.documentElement;
        const panels = [
          ...document.querySelectorAll<HTMLElement>('[role="tabpanel"]'),
        ]
          .filter((panel) => !panel.hidden)
          .map((panel) => panel.getBoundingClientRect());
        return (
          root.scrollWidth <= innerWidth + 1 &&
          root.scrollHeight <= innerHeight + 1 &&
          panels.every(
            (box) =>
              box.width > 0 &&
              box.height > 0 &&
              box.left >= -1 &&
              box.top >= -1 &&
              box.right <= innerWidth + 1 &&
              box.bottom <= innerHeight + 1,
          )
        );
      }),
    )
    .toBe(true);
}
