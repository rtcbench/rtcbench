import { test, expect, uniqueName } from "./fixtures";

test("benchmark simulation persists, downloads evidence and runs again", async ({
  page,
  request,
}) => {
  const name = uniqueName("capacity");
  const ids: string[] = [];
  try {
    await page.goto("benchmarks");
    await page
      .getByRole("button", { name: "New benchmark", exact: true })
      .click();
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(name);
    await page
      .getByRole("spinbutton", { name: "Maximum per machine" })
      .fill("24");
    await page
      .getByRole("checkbox", {
        name: "Simulate search without contacting machines",
      })
      .check();
    const started = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/v1/benchmarks") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Simulate", exact: true }).click();
    const response = await started;
    expect(response.status(), await response.text()).toBe(201);
    ids.push((await response.json()).id);
    await expect(
      page.getByText("Simulation — no measurements", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Run again", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("img", { name: "Highest passing load by client" }),
    ).toContainText("20");
    await page.reload();
    await expect(
      page.getByRole("heading", { name, exact: true }),
    ).toBeVisible();
    const result = await (
      await request.get(`api/v1/benchmarks/${ids[0]}`)
    ).json();
    expect(result.report.results["livekit/rtcbench"].best).toBe(20);
    expect(
      result.report.trials.every(
        (t: { reason: string }) => t.reason === "SIMULATED",
      ),
    ).toBe(true);
    const downloadPromise = page.waitForEvent("download");
    await page
      .getByRole("link", { name: "experiments.jsonl", exact: true })
      .first()
      .click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe("experiments.jsonl");
    expect(await download.failure()).toBeNull();
    await page.getByText("All artifacts", { exact: true }).click();
    await expect(
      page.getByRole("link", { name: "cluster.json", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Run again", exact: true }).click();
    await expect(
      page.getByRole("textbox", { name: "Name", exact: true }),
    ).toHaveValue(name);
    await page
      .getByRole("textbox", { name: "Name", exact: true })
      .fill(name + "-again");
    const repeated = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/v1/benchmarks") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Simulate", exact: true }).click();
    ids.push((await (await repeated).json()).id);
    await expect(
      page.getByRole("heading", { name: name + "-again", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Run again", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    await page
      .getByRole("alertdialog")
      .getByRole("button", { name: "Delete", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Benchmarks", exact: true }),
    ).toBeVisible();
    expect((await request.get(`api/v1/benchmarks/${ids[1]}`)).status()).toBe(
      404,
    );
  } finally {
    for (const id of ids) await request.delete(`api/v1/benchmarks/${id}`);
  }
});

test("invalid benchmark setup stays editable and does not create a job", async ({
  page,
}) => {
  await page.goto("benchmarks/new");
  await page
    .getByRole("spinbutton", { name: "Minimum per machine" })
    .fill("50");
  await page
    .getByRole("spinbutton", { name: "Maximum per machine" })
    .fill("10");
  await page
    .getByRole("checkbox", {
      name: "Simulate search without contacting machines",
    })
    .check();
  await page.getByRole("button", { name: "Simulate", exact: true }).click();
  await expect(
    page.getByText("invalid search bounds", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Simulate", exact: true }),
  ).toBeEnabled();
});
