import { test, expect, uniqueName, select } from "./fixtures";
import { readFile } from "node:fs/promises";

test("config create, edit, reload and delete preserve list/detail tabs", async ({
  page,
  request,
}) => {
  const name = uniqueName("config");
  const yaml =
    "apiVersion: rtcbench/v1\nkind: VideoCallStressTest\nspec:\n  plugin: janus\n";
  try {
    await page.goto("configs");
    await page.getByRole("button", { name: "New config", exact: true }).click();
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(name);
    await page.getByRole("textbox", { name: "YAML", exact: true }).fill(yaml);
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      page.getByRole("heading", { name, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("tab", { name: "Configs", exact: true }),
    ).toHaveCount(1);
    await expect(page.getByRole("tab", { name, exact: true })).toHaveCount(1);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page
      .getByRole("textbox", { name: "YAML", exact: true })
      .fill(yaml + "  scenario: custom\n");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Edit", exact: true }),
    ).toBeVisible();
    await page.reload();
    expect(
      (await (await request.get(`api/v1/configs/${name}`)).json()).yaml,
    ).toContain("scenario: custom");
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    const dialog = page.getByRole("alertdialog");
    await dialog.getByRole("textbox").fill(name);
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Configs", exact: true }),
    ).toBeVisible();
    expect((await request.get(`api/v1/configs/${name}`)).status()).toBe(404);
  } finally {
    await request.delete(`api/v1/configs/${name}`);
  }
});

test("closing an unsaved config offers keep editing or discard", async ({
  page,
}) => {
  await page.goto("configs/new");
  await page
    .getByRole("textbox", { name: "Name", exact: true })
    .fill(uniqueName("draft"));
  await page
    .getByRole("button", { name: "Close New config", exact: true })
    .click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep tab", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "Name", exact: true }),
  ).not.toBeEmpty();
  await page
    .getByRole("button", { name: "Close New config", exact: true })
    .click();
  await dialog.getByRole("button", { name: /discard/i }).click();
  await expect(
    page.getByRole("tab", { name: "New config", exact: true }),
  ).toHaveCount(0);
});

test("network created in deployment setup returns to the preserved draft", async ({
  page,
  request,
}) => {
  const name = uniqueName("network");
  let id: string | undefined;
  try {
    await page.goto("systems/deployments/new");
    await page
      .getByRole("textbox", { name: "Name", exact: true })
      .fill("Preserved deployment");
    await select(page, "Setup", "Connect to existing");
    await page
      .getByRole("button", { name: "Add network", exact: true })
      .click();
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(name);
    await page
      .getByRole("textbox", { name: "CIDR", exact: true })
      .fill("10.99.0.0/24");
    const saved = page.waitForResponse(
      (r) =>
        r.url().endsWith("/systems/networks") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Save", exact: true }).click();
    const response = await saved;
    expect(response.ok()).toBe(true);
    id = (await response.json()).id;
    await expect(
      page.getByRole("textbox", { name: "Name", exact: true }),
    ).toHaveValue("Preserved deployment");
    await expect(
      page.getByRole("combobox", { name: "Network", exact: true }),
    ).toHaveText(name);
  } finally {
    if (id)
      expect((await request.delete(`api/v1/systems/networks/${id}`)).ok()).toBe(
        true,
      );
  }
});

test("custom deployment ports persist and drive benchmark overrides", async ({
  page,
  request,
}) => {
  const name = uniqueName("deployment");
  let id: string | undefined;
  try {
    await page.goto("systems/deployments/new");
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(name);
    await select(page, "Plugin", "Janus");
    await select(page, "Setup", "Connect to existing");
    await page
      .getByRole("textbox", { name: "Traffic IP", exact: true })
      .fill("192.0.2.10");
    await page.getByText("Ports", { exact: true }).click();
    await expect(
      page.getByRole("spinbutton", { name: "Signaling (TCP)", exact: true }),
    ).toHaveValue("8088");
    await page
      .getByRole("spinbutton", { name: "Signaling (TCP)", exact: true })
      .fill("9088");
    const saved = page.waitForResponse(
      (r) =>
        r.url().endsWith("/deployments") && r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Connect", exact: true }).click();
    const response = await saved;
    expect(response.ok()).toBe(true);
    id = (await response.json()).id;
    await expect(
      page.getByRole("heading", { name, exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByText("http://192.0.2.10:9088/janus", { exact: true }),
    ).toBeVisible();
    const inventory = await (await request.get("api/v1/systems")).json();
    expect(
      inventory.deployments.find((d: { id: string }) => d.id === id).ports
        .signaling,
    ).toBe(9088);
  } finally {
    if (id)
      expect((await request.delete(`api/v1/deployments/${id}`)).ok()).toBe(
        true,
      );
  }
});

test("plugin generator downloads a complete project through the proxy", async ({
  page,
}) => {
  const name = uniqueName("plugin");
  await page.goto("plugins");
  await expect(
    page.getByRole("table", { name: "Plugins", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "New Plugin", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Project name", exact: true })
    .fill(name);
  await expect(
    page.getByRole("textbox", { name: "Go module", exact: true }),
  ).toHaveValue(`example.com/${name}`);
  await page
    .getByRole("button", { name: "Create plugin", exact: true })
    .click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
  const download = page.waitForEvent("download");
  await page
    .getByRole("link", { name: "Download project", exact: true })
    .click();
  const file = await download;
  expect(await file.failure()).toBeNull();
  expect(file.suggestedFilename()).toBe(`${name}.zip`);
  const bytes = await readFile((await file.path())!);
  const archive = new TextDecoder().decode(bytes);
  for (const path of [
    "plugin/plugin.go",
    "plugin.yml",
    "main.go",
    "go.mod",
    "Makefile",
    "Dockerfile",
    "web/main.tsx",
    "web/package.json",
  ]) {
    expect(archive).toContain(path);
  }
  await page.goto("plugins");
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
});
