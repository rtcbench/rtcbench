import { test, expect, tabAction, uniqueName } from "./fixtures";

test("browser history restores duplicate tabs inside the proxy basename", async ({
  page,
}) => {
  await page.goto("configs?view=list#saved");
  const original = page.getByRole("tab", { name: "Configs", exact: true });
  await expect(original).toHaveAttribute("aria-selected", "true");
  const originalID = await original.getAttribute("id");
  await expect
    .poll(() => page.evaluate(() => history.state?.usr?.tabKey))
    .toBe("configs");
  await expect(page).toHaveURL(/\/app\/configs\?view=list#saved$/);
  await tabAction(original, "Duplicate tab");
  const copies = page.getByRole("tab", { name: "Configs", exact: true });
  await expect(copies).toHaveCount(2);
  const selected = page.getByRole("tab", { selected: true });
  const copyID = await selected.getAttribute("id");
  expect(copyID).not.toBe(originalID);
  await page.goBack();
  await expect(selected).toHaveAttribute("id", originalID!);
  await expect(page).toHaveURL(/\/app\/configs\?view=list#saved$/);
  await page.goForward();
  await expect(selected).toHaveAttribute("id", copyID!);
  await page.reload();
  await expect(copies).toHaveCount(2);
  await expect(selected).toHaveAttribute("id", copyID!);
});

test("direct links decode document names once and survive reload", async ({
  page,
  request,
}) => {
  const name = `${uniqueName("route")}/Research %2F %25`;
  const path = name.split("/").map(encodeURIComponent).join("/");
  const result = await request.post("api/v1/configs", {
    data: {
      name,
      yaml: "apiVersion: rtcbench/v1\nkind: VideoCallStressTest\nspec:\n  plugin: janus\n",
    },
  });
  expect(result.ok()).toBe(true);
  try {
    await page.goto(`config/${path}`);
    await expect(
      page.getByRole("heading", { name, exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("heading", { name, exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("tab", { name, exact: true })).toHaveCount(1);
  } finally {
    await request.delete(`api/v1/configs/${path}`);
  }
});

test("unsupported deep links show a missing page with working navigation", async ({
  page,
}) => {
  await page.goto("runs/unsupported");
  await expect(
    page.getByRole("heading", { name: "Page not found", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/app\/runs\/unsupported$/);
  await page.getByRole("link", { name: "Runs", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Runs", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/app\/runs$/);
});

test("restores duplicate tab selection from the previous history format", async ({
  page,
}) => {
  await page.addInitScript(() => {
    sessionStorage.setItem(
      "rtcbench:tabs:/app/",
      JSON.stringify([
        { key: "first", id: "configs", path: "configs" },
        { key: "second", id: "configs", path: "configs" },
      ]),
    );
    history.replaceState({ tabKey: "second" }, "");
  });
  await page.goto("configs");
  await expect(
    page.getByRole("tab", { name: "Configs", exact: true }),
  ).toHaveCount(2);
  await expect(page.getByRole("tab", { selected: true })).toHaveAttribute(
    "data-tab",
    "second",
  );
});
