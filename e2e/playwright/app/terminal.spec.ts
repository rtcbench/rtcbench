import { test, expect } from "./fixtures";

test("disabled terminals retry once then explain the required setting", async ({
  page,
}) => {
  test.skip(
    !!process.env.RTCBENCH_BASE_URL,
    "Uses the isolated server's default settings",
  );
  let attempts = 0;
  page.on("response", (response) => {
    if (new URL(response.url()).pathname.endsWith("/api/v1/terminal"))
      attempts++;
  });
  await page.goto("terminal");
  await expect(page.getByRole("alert")).toContainText("terminal.server");
  expect(attempts).toBe(2);
  await expect(
    page.getByRole("alert").getByRole("button", { name: "Reconnect terminal" }),
  ).toBeVisible();
});

test("first terminal opens below and the split survives a reload", async ({
  page,
}) => {
  await page.goto("welcome");
  await page
    .locator("[data-pane-header]")
    .getByRole("button", { name: "Open a tab", exact: true })
    .click();
  await page
    .getByRole("menuitem", { name: "New Terminal", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Open terminal", exact: true })
    .click();
  await expect(page.getByRole("tabpanel")).toHaveCount(2);
  await expect
    .poll(async () => {
      const [first, second] = await Promise.all([
        page.getByRole("tabpanel").first().boundingBox(),
        page.getByRole("tabpanel").last().boundingBox(),
      ]);
      return second!.y - first!.y - first!.height;
    })
    .toBeGreaterThan(0);
  await page.reload();
  await expect(page.getByRole("tabpanel")).toHaveCount(2);
});

for (const kind of ["server", "instances", "deployments"] as const) {
  test(`${kind} terminal executes input over the authenticated WebSocket`, async ({
    page,
    request,
  }) => {
    test.skip(
      !process.env.RTCBENCH_TERMINAL_TESTS,
      "Needs an enabled Linux test controller and managed deployment",
    );
    const inventory = await (await request.get("api/v1/systems")).json();
    const deployment = inventory.deployments.find(
      (d: { hostId?: string; status: string }) =>
        d.hostId && d.status === "ready",
    );
    if (kind !== "server")
      expect(deployment, "A ready managed deployment is required").toBeTruthy();
    const target =
      kind === "server"
        ? "terminal"
        : `terminal/${kind}/${kind === "instances" ? deployment.hostId : deployment.id}`;
    let output = "";
    let connections = 0;
    page.on("websocket", (socket) => {
      connections++;
      socket.on("framereceived", ({ payload }) => {
        output += payload.toString();
      });
    });
    await page.goto(target);
    await expect(
      page.getByRole("status").filter({ hasText: /^Connected$/ }),
    ).toHaveCount(1);
    await page
      .getByRole("group", { name: "Terminal input", exact: true })
      .click();
    await expect(
      page.getByRole("textbox", { name: "Terminal input", exact: true }),
    ).toBeFocused();
    await page.keyboard.type("printf 'RTC%s\\n' 'BENCH_E2E'");
    await page.keyboard.press("Enter");
    await expect.poll(() => output).toContain("RTCBENCH_E2E");
    await expect(page.getByRole("alert")).toHaveCount(0);
    const before = connections;
    await page.getByRole("button", { name: /^Reconnect / }).click();
    await expect.poll(() => connections).toBe(before + 1);
    await expect(
      page.getByRole("status").filter({ hasText: /^Connected$/ }),
    ).toHaveCount(1);
    await expect(page.getByRole("alert")).toHaveCount(0);
  });
}
